package control

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// AMIConfig describes one Asterisk Manager Interface connection.
type AMIConfig struct {
	// Addr is host:port of the manager interface (Asterisk's default is :5038).
	Addr string
	// Username and Secret are the manager.conf credentials.
	Username string
	Secret   string
	// Context is the dialplan context route/transfer redirect into when the
	// action does not name one. Defaults to "default".
	Context string
	// Timeout bounds one action when its context carries no deadline of its
	// own. Defaults to 5s.
	Timeout time.Duration
}

// AMI is a connection to the Asterisk Manager Interface. It carries actions out
// and, because the dialplan announces each call with a UserEvent, it is also
// what resolves an AudioSocket UUID to the channel the call is on.
//
// AMI is line-based TCP, so this needs nothing beyond the standard library
// (ADR-0007).
type AMI struct {
	cfg  AMIConfig
	conn net.Conn
	br   *bufio.Reader

	wmu sync.Mutex // serialises writes; AMI blocks must not interleave

	nextID atomic.Int64

	pmu     sync.Mutex
	pending map[string]chan map[string]string

	cmu      sync.RWMutex
	byUUID   map[string]CallInfo // AudioSocket UUID -> what the dialplan said
	byChan   map[string]string   // channel -> UUID, so Hangup can evict both
	closed   chan struct{}
	closeErr error
	once     sync.Once
}

// startEvent is the dialplan UserEvent that announces a call to the gateway.
const startEvent = "IvrvgStart"

// DialAMI connects, reads the banner, and logs in.
func DialAMI(ctx context.Context, cfg AMIConfig) (*AMI, error) {
	if cfg.Context == "" {
		cfg.Context = "default"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("control: ami dial %s: %w", cfg.Addr, err)
	}
	a := &AMI{
		cfg:     cfg,
		conn:    conn,
		br:      bufio.NewReader(conn),
		pending: make(map[string]chan map[string]string),
		byUUID:  make(map[string]CallInfo),
		byChan:  make(map[string]string),
		closed:  make(chan struct{}),
	}

	banner, err := a.br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("control: ami banner: %w", err)
	}
	if !strings.HasPrefix(banner, "Asterisk Call Manager") {
		conn.Close()
		return nil, fmt.Errorf("control: ami: not a manager interface (got %q)", strings.TrimSpace(banner))
	}

	go a.readLoop()

	if _, err := a.call(ctx, []field{
		{"Action", "Login"},
		{"Username", cfg.Username},
		{"Secret", cfg.Secret},
	}); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}

// CallInfo is what the dialplan announced about one call as it entered the
// media adapter.
type CallInfo struct {
	// Channel is the Asterisk channel name, which actions are addressed to.
	Channel string
	// Hint is the IVR's context for the call — caller id, menu choice,
	// account — passed straight through to the brain.
	Hint string
}

// Lookup returns what the dialplan announced for an AudioSocket UUID, once its
// UserEvent has arrived.
func (a *AMI) Lookup(uuid string) (CallInfo, bool) {
	a.cmu.RLock()
	defer a.cmu.RUnlock()
	info, ok := a.byUUID[uuid]
	return info, ok
}

// Do carries one action out on the given channel (ADR-0007 has the mapping).
func (a *AMI) Do(ctx context.Context, channel string, act Action) error {
	if channel == "" {
		return fmt.Errorf("control: ami %s: no channel for this call", act.Kind)
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	switch act.Kind {
	case SendDTMF:
		digit := arg(act.Args, "digit")
		if digit == "" {
			return fmt.Errorf("control: send_dtmf: no digit given")
		}
		return a.do(ctx, []field{{"Action", "PlayDTMF"}, {"Channel", channel}, {"Digit", digit}})

	case Route, Transfer:
		exten := arg(act.Args, "dest", "exten")
		if exten == "" {
			return fmt.Errorf("control: %s: no destination given", act.Kind)
		}
		dialplanCtx := arg(act.Args, "context")
		if dialplanCtx == "" {
			dialplanCtx = a.cfg.Context
		}
		priority := arg(act.Args, "priority")
		if priority == "" {
			priority = "1"
		}
		return a.do(ctx, []field{
			{"Action", "Redirect"}, {"Channel", channel},
			{"Exten", exten}, {"Context", dialplanCtx}, {"Priority", priority},
		})

	case Park:
		return a.do(ctx, []field{{"Action", "Park"}, {"Channel", channel}})

	case Hangup:
		return a.do(ctx, []field{{"Action", "Hangup"}, {"Channel", channel}})

	case Escalate:
		// Escalate is an attended transfer: redirect the channel to the human
		// agent's extension. The handoff packet travels over the console
		// WebSocket, not AMI.
		exten := arg(act.Args, "dest", "exten")
		if exten == "" {
			return nil // no switch-side action; the handoff is on the console
		}
		dialplanCtx := arg(act.Args, "context")
		if dialplanCtx == "" {
			dialplanCtx = a.cfg.Context
		}
		return a.do(ctx, []field{
			{"Action", "Redirect"}, {"Channel", channel},
			{"Exten", exten}, {"Context", dialplanCtx}, {"Priority", "1"},
		})

	case ReadDigits:
		// Digits arrive over AudioSocket and the engine collects them; there is
		// nothing for the switch to do.
		return nil

	case PlayPrompt:
		// Reaching Playback() means redirecting the channel out of AudioSocket,
		// which would end the media session the prompt interrupts. ARI does
		// this properly in Phase 3.
		return fmt.Errorf("%w: play_prompt needs ARI", ErrUnsupported)
	}
	return fmt.Errorf("control: ami: unknown action %q", act.Kind)
}

// Originate places an outbound call via AMI.
func (a *AMI) Originate(ctx context.Context, o Originate) error {
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	fields := []field{
		{"Action", "Originate"},
	}
	if o.Channel != "" {
		fields = append(fields, field{"Channel", o.Channel})
	}
	if o.Exten != "" {
		fields = append(fields, field{"Exten", o.Exten})
	}
	if o.Context != "" {
		fields = append(fields, field{"Context", o.Context})
	}
	if o.Priority != 0 {
		fields = append(fields, field{"Priority", strconv.Itoa(o.Priority)})
	}
	if o.CallerID != "" {
		fields = append(fields, field{"CallerID", o.CallerID})
	}
	if o.Timeout > 0 {
		fields = append(fields, field{"Timeout", strconv.Itoa(o.Timeout)})
	}
	if o.Async {
		fields = append(fields, field{"Async", "true"})
	}
	if o.Application != "" {
		fields = append(fields, field{"Application", o.Application})
	}
	if o.Data != "" {
		fields = append(fields, field{"Data", o.Data})
	}
	for k, v := range o.Variables {
		if strings.ContainsAny(k, "=\r\n") {
			return fmt.Errorf("control: ami originate: invalid variable name %q", k)
		}
		fields = append(fields, field{"Variable", k + "=" + v})
	}
	for _, f := range fields {
		if strings.ContainsAny(f.v, "\r\n") {
			return fmt.Errorf("control: ami originate: line break in %s", f.k)
		}
	}
	_, err := a.call(ctx, fields)
	return err
}

// Close logs off and closes the connection. It is safe to call more than once.
func (a *AMI) Close() error {
	a.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = a.call(ctx, []field{{"Action", "Logoff"}}) // best effort
		a.conn.Close()
	})
	<-a.closed
	if errors.Is(a.closeErr, net.ErrClosed) {
		return nil
	}
	return a.closeErr
}

type field struct{ k, v string }

func (a *AMI) do(ctx context.Context, fields []field) error {
	_, err := a.call(ctx, fields)
	return err
}

// call writes one action block and waits for the response carrying its ActionID.
func (a *AMI) call(ctx context.Context, fields []field) (map[string]string, error) {
	id := strconv.FormatInt(a.nextID.Add(1), 10)
	reply := make(chan map[string]string, 1)

	a.pmu.Lock()
	a.pending[id] = reply
	a.pmu.Unlock()
	defer func() {
		a.pmu.Lock()
		delete(a.pending, id)
		a.pmu.Unlock()
	}()

	var b strings.Builder
	for _, f := range fields {
		fmt.Fprintf(&b, "%s: %s\r\n", f.k, f.v)
	}
	fmt.Fprintf(&b, "ActionID: %s\r\n\r\n", id)

	a.wmu.Lock()
	_, err := a.conn.Write([]byte(b.String()))
	a.wmu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("control: ami write: %w", err)
	}

	action := fields[0].v
	select {
	case resp := <-reply:
		if !strings.EqualFold(resp["response"], "Success") {
			msg := resp["message"]
			if msg == "" {
				msg = resp["response"]
			}
			return resp, fmt.Errorf("control: ami %s refused: %s", action, msg)
		}
		return resp, nil
	case <-a.closed:
		return nil, fmt.Errorf("control: ami %s: connection closed", action)
	case <-ctx.Done():
		return nil, fmt.Errorf("control: ami %s: %w", action, ctx.Err())
	}
}

func (a *AMI) readLoop() {
	defer close(a.closed)
	for {
		blk, err := readBlock(a.br)
		if err != nil {
			a.closeErr = err
			return
		}
		if _, isResponse := blk["response"]; isResponse {
			a.deliver(blk["actionid"], blk)
			continue
		}
		if blk["event"] != "" {
			a.handleEvent(blk)
		}
	}
}

func (a *AMI) deliver(id string, blk map[string]string) {
	if id == "" {
		return
	}
	a.pmu.Lock()
	ch := a.pending[id]
	a.pmu.Unlock()
	if ch != nil {
		select {
		case ch <- blk:
		default: // the caller gave up; nothing to do
		}
	}
}

// handleEvent keeps the UUID-to-channel index current. The dialplan raises
// startEvent as the call enters AudioSocket, and Hangup evicts it again so the
// index cannot grow without bound.
func (a *AMI) handleEvent(blk map[string]string) {
	switch {
	case strings.EqualFold(blk["event"], "UserEvent") && strings.EqualFold(blk["userevent"], startEvent):
		uuid, channel := blk["uuid"], blk["channel"]
		if uuid == "" || channel == "" {
			return
		}
		a.cmu.Lock()
		a.byUUID[uuid] = CallInfo{Channel: channel, Hint: blk["hint"]}
		a.byChan[channel] = uuid
		a.cmu.Unlock()

	case strings.EqualFold(blk["event"], "Hangup"):
		channel := blk["channel"]
		if channel == "" {
			return
		}
		a.cmu.Lock()
		if uuid, ok := a.byChan[channel]; ok {
			delete(a.byUUID, uuid)
			delete(a.byChan, channel)
		}
		a.cmu.Unlock()
	}
}

func (a *AMI) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, a.cfg.Timeout)
}

// readBlock reads one AMI message: "Key: Value" lines ended by a blank line.
// Keys are lowercased because Asterisk's capitalisation varies by version.
func readBlock(r *bufio.Reader) (map[string]string, error) {
	blk := make(map[string]string)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(blk) == 0 {
				continue // tolerate padding between messages
			}
			return blk, nil
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue // command output lines carry no key
		}
		blk[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
}

// arg returns the first of names present in args as a string.
func arg(args map[string]any, names ...string) string {
	for _, n := range names {
		v, ok := args[n]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case string:
			return t
		case float64: // JSON numbers decode to float64
			return strconv.FormatFloat(t, 'f', -1, 64)
		case int:
			return strconv.Itoa(t)
		}
	}
	return ""
}
