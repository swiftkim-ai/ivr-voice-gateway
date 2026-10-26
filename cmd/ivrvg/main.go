// Command ivrvg is the IVR Voice Gateway: it serves calls over the v1 contract,
// replays recorded calls deterministically, maps remote IVRs, and diffs the
// maps to catch regressions.
//
//	ivrvg serve   [-addr :8099] [-logdir data/calls] [-audiosocket :8090] [-ami host:5038]
//	ivrvg dial    -ami host:5038 -channel 'PJSIP/{number}@trunk' [-ami-user ivrvg] [-out-context ivrvg-out] <number>
//	ivrvg map     -ami host:5038 -channel 'PJSIP/{number}@trunk' -scenario scenarios.json [-audiosocket :8090] [-out map.json] [-logdir data/outbound]
//	ivrvg diff    <before.json> <after.json>
//	ivrvg replay  <call.jsonl>
//	ivrvg sample  [-out testdata/calls/example.jsonl]
//	ivrvg version
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/call"
	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
	"github.com/swiftkimani/ivr-voice-gateway/internal/flowmap"
	"github.com/swiftkimani/ivr-voice-gateway/internal/gateway"
	"github.com/swiftkimani/ivr-voice-gateway/internal/replay"
	"github.com/swiftkimani/ivr-voice-gateway/internal/scenario"
)

const version = "0.2.0"

// Secrets come from the environment, never from flags, which are visible to
// anyone who can list processes.
const (
	saltEnv      = "IVRVG_REDACTION_SALT"
	amiSecretEnv = "IVRVG_AMI_SECRET"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe(os.Args[2:])
	case "dial":
		err = cmdDial(os.Args[2:])
	case "map":
		err = cmdMap(os.Args[2:])
	case "diff":
		err = cmdDiff(os.Args[2:])
	case "replay":
		err = cmdReplay(os.Args[2:])
	case "sample":
		err = cmdSample(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("ivrvg", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `ivrvg — IVR Voice Gateway

  serve   [-addr :8099] [-logdir data/calls]
          [-audiosocket :8090] [-ami host:5038] [-ami-user ivrvg] [-ami-context default]
  dial    -ami host:5038 -channel 'PJSIP/{number}@trunk' [-ami-user ivrvg] [-out-context ivrvg-out] [-callerid ...] <number>
  map     -ami host:5038 -channel 'PJSIP/{number}@trunk' -scenario file.json
          [-audiosocket :8090] [-out map.json] [-logdir data/outbound]
  diff    <before.json> <after.json>
  replay  <call.jsonl>
  sample  [-out testdata/calls/example.jsonl]
  version

Environment:
  IVRVG_AMI_SECRET       Asterisk manager password; required with -ami.
  IVRVG_REDACTION_SALT   Key for sensitive digit tokens. A random one is
                         generated per run when unset, so tokens stop
                         correlating across restarts.
`)
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8099", "listen address for the media WebSocket")
	logdir := fs.String("logdir", "data/calls", "directory for per-call event logs")
	asAddr := fs.String("audiosocket", "", "listen address for Asterisk AudioSocket (empty: off)")
	amiAddr := fs.String("ami", "", "Asterisk manager address host:port (empty: off)")
	amiUser := fs.String("ami-user", "ivrvg", "Asterisk manager username")
	amiContext := fs.String("ami-context", "default", "dialplan context that route and transfer redirect into")
	_ = fs.Parse(args)

	redactor, generated := redactionKey()
	if generated {
		log.Printf("ivrvg: %s is unset; using a random key for this run, so digit tokens will not correlate across restarts", saltEnv)
	}

	var (
		ctl      control.Controller
		channels gateway.ChannelLookup
	)
	if *amiAddr != "" {
		ami, err := dialManager(*amiAddr, *amiUser, *amiContext)
		if err != nil {
			return err
		}
		defer ami.Close()
		ctl, channels = ami, ami
		log.Printf("ivrvg: manager interface connected at %s (redirects into context %q)", *amiAddr, *amiContext)
	}

	// Either listener failing ends the process; neither is optional once asked for.
	errs := make(chan error, 2)

	if *asAddr != "" {
		opts := gateway.AudioSocketOptions{
			LogDir: *logdir, Brain: demoBrain(), Speaker: brain.Silence{},
			Control: ctl, Channels: channels, Redactor: redactor,
		}
		go func() { errs <- gateway.ListenAndServeAudioSocket(*asAddr, opts) }()
		log.Printf("ivrvg: audiosocket on %s (PCM16 8k, 20 ms frames)", *asAddr)
	}

	h := gateway.Handler(gateway.Options{
		LogDir: *logdir, Brain: demoBrain(), Speaker: brain.Silence{}, Redactor: redactor,
	})
	go func() { errs <- http.ListenAndServe(*addr, h) }()
	log.Printf("ivrvg %s listening on %s (ws /v1/media, logs -> %s)", version, *addr, *logdir)

	return <-errs
}

func dialManager(addr, user, dialplanContext string) (*control.AMI, error) {
	secret := os.Getenv(amiSecretEnv)
	if secret == "" {
		return nil, fmt.Errorf("serve: -ami needs %s to be set", amiSecretEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return control.DialAMI(ctx, control.AMIConfig{
		Addr: addr, Username: user, Secret: secret, Context: dialplanContext,
	})
}

// redactionKey returns the configured key for sensitive digit tokens. When none
// is set it generates one for this run: the digits still never reach the log,
// but tokens no longer match across restarts.
func redactionKey() (*call.Redactor, bool) {
	if s := os.Getenv(saltEnv); s != "" {
		return call.NewRedactor(s), false
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is not survivable: without a key the engine
		// refuses sensitive groups outright.
		log.Fatalf("ivrvg: cannot generate a redaction key: %v", err)
	}
	return call.NewRedactor(hex.EncodeToString(b)), true
}

func cmdReplay(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("replay: a call log path is required")
	}
	events, err := eventlog.Read(args[0])
	if err != nil {
		return err
	}
	rep, err := replay.Run(context.Background(), events, demoBrain(), brain.Silence{})
	if err != nil {
		return err
	}
	fmt.Printf("call %s: %d turns, %d divergences\n", rep.CallID, rep.Turns, len(rep.Divergences))
	for _, d := range rep.Divergences {
		fmt.Printf("  step %d (caller %q) %s: expected %q, got %q\n", d.Step, d.Caller, d.Field, d.Expect, d.Got)
	}
	if !rep.OK() {
		os.Exit(1)
	}
	fmt.Println("replay OK")
	return nil
}

// cmdSample runs a scripted call through the engine and writes its event log, so
// there is a real, replayable recording to demo and test against.
func cmdSample(args []string) error {
	fs := flag.NewFlagSet("sample", flag.ExitOnError)
	out := fs.String("out", "testdata/calls/example.jsonl", "output call log path")
	_ = fs.Parse(args)

	const callID = "sample-0001"
	lg, err := eventlog.Create(*out, callID)
	if err != nil {
		return err
	}
	defer lg.Close()

	ctx := context.Background()
	c := &call.Call{
		ID: callID, Hint: "+15550001111",
		Brain: demoBrain(), Speaker: brain.Silence{},
		Sink: &call.MemorySink{}, Log: lg,
		Redactor: call.NewRedactor("ivrvg-sample"),
	}
	if err := c.Start(); err != nil {
		return err
	}
	if err := c.Respond(ctx); err != nil { // greeting
		return err
	}
	if err := c.Digit(ctx, "1", false); err != nil {
		return err
	}
	if err := c.Input(ctx, "I have a question about my last invoice"); err != nil {
		return err
	}
	if err := c.End("hangup"); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", *out)
	return nil
}

// demoBrain is the scripted Phase 0 brain. Specific rules come first so the "*"
// fallback only serves the greeting and unrecognised input.
func demoBrain() brain.Brain {
	return &brain.Scripted{Rules: []brain.Rule{
		// First: a collected group contains digits that would otherwise match
		// the single-digit rules below.
		{Match: "digits:", Say: []string{"I have account {caller}. Connecting you to sales now."},
			Actions: []control.Action{{Kind: control.Transfer, Args: map[string]any{"dest": "200"}}}},
		{Match: "1", Say: []string{"Connecting you to sales now."},
			Actions: []control.Action{{Kind: control.Transfer, Args: map[string]any{"dest": "200"}}}},
		{Match: "2", Say: []string{"Sure. Enter your account number, then press hash."},
			Actions: []control.Action{{Kind: control.ReadDigits, Args: map[string]any{
				"label": "account", "terminator": "#",
			}}}},
		{Match: "0", Say: []string{"I will put you through to an operator."},
			Actions: []control.Action{{Kind: control.Transfer, Args: map[string]any{"dest": "100"}}}},
		{Match: "*", Say: []string{"Hello, you have reached the gateway. Press 1 for sales, 2 to book, or 0 for an operator."}},
	}}
}

const outboundContext = "ivrvg-out"

func cmdDial(args []string) error {
	fs := flag.NewFlagSet("dial", flag.ExitOnError)
	amiAddr := fs.String("ami", "", "Asterisk manager address host:port")
	amiUser := fs.String("ami-user", "ivrvg", "Asterisk manager username")
	channelTemplate := fs.String("channel", "", "Asterisk channel template with one {number}, e.g. PJSIP/{number}@trunk")
	outContext := fs.String("out-context", outboundContext, "dialplan context run when the called party answers")
	callerID := fs.String("callerid", "ivrvg", "caller id to present")
	_ = fs.Parse(args)
	if len(fs.Args()) < 1 {
		return fmt.Errorf("dial: a number is required")
	}
	if *amiAddr == "" {
		return fmt.Errorf("dial: -ami is required")
	}
	number := fs.Args()[0]
	channel, err := outboundChannel(*channelTemplate, number)
	if err != nil {
		return err
	}
	callID, err := newCallID()
	if err != nil {
		return fmt.Errorf("dial: generate call id: %w", err)
	}
	ami, err := dialManager(*amiAddr, *amiUser, "default")
	if err != nil {
		return err
	}
	defer ami.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ami.Originate(ctx, control.Originate{
		Channel:   channel,
		Context:   *outContext,
		Exten:     "s",
		Priority:  1,
		CallerID:  *callerID,
		Timeout:   30000,
		Async:     true,
		Variables: map[string]string{"IVRVG_UUID": callID},
	}); err != nil {
		return fmt.Errorf("dial: originate call %s: %w", callID, err)
	}
	fmt.Printf("origination accepted (call %s)\n", callID)
	return nil
}

func cmdMap(args []string) error {
	fs := flag.NewFlagSet("map", flag.ExitOnError)
	amiAddr := fs.String("ami", "", "Asterisk manager address host:port")
	amiUser := fs.String("ami-user", "ivrvg", "Asterisk manager username")
	channelTemplate := fs.String("channel", "", "Asterisk channel template with one {number}")
	asAddr := fs.String("audiosocket", ":8090", "listen address for AudioSocket")
	outContext := fs.String("out-context", outboundContext, "dialplan context for outbound calls")
	callerID := fs.String("callerid", "ivrvg", "caller id to present")
	scenarioFile := fs.String("scenario", "", "scenario file (JSON)")
	outFile := fs.String("out", "map.json", "output flow map file")
	logdir := fs.String("logdir", "data/outbound", "directory for per-call event logs")
	acceptTimeout := fs.Duration("accept-timeout", 30*time.Second, "how long to wait for each call to connect")
	_ = fs.Parse(args)

	if *scenarioFile == "" {
		return fmt.Errorf("map: -scenario is required")
	}
	if *amiAddr == "" {
		return fmt.Errorf("map: -ami is required")
	}
	if *channelTemplate == "" {
		return fmt.Errorf("map: -channel is required")
	}

	scenarios, err := scenario.Load(*scenarioFile)
	if err != nil {
		return err
	}
	log.Printf("ivrvg map: loaded %d scenario(s) from %s", len(scenarios), *scenarioFile)

	ami, err := dialManager(*amiAddr, *amiUser, "default")
	if err != nil {
		return err
	}
	defer ami.Close()

	var allLogs [][]eventlog.Event

	for i, sc := range scenarios {
		target := sc.Target
		if target == "" {
			return fmt.Errorf("map: scenario %q has no target number", sc.Name)
		}

		channel, err := outboundChannel(*channelTemplate, target)
		if err != nil {
			return err
		}

		callID, err := newCallID()
		if err != nil {
			return fmt.Errorf("map: generate call id: %w", err)
		}

		ln, err := net.Listen("tcp", *asAddr)
		if err != nil {
			return fmt.Errorf("map: listen %s: %w", *asAddr, err)
		}

		log.Printf("ivrvg map: [%d/%d] %s — originating %s (call %s)", i+1, len(scenarios), sc.Name, target, callID)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if err := ami.Originate(ctx, control.Originate{
			Channel:   channel,
			Context:   *outContext,
			Exten:     "s",
			Priority:  1,
			CallerID:  *callerID,
			Timeout:   30000,
			Async:     true,
			Variables: map[string]string{"IVRVG_UUID": callID},
		}); err != nil {
			cancel()
			ln.Close()
			return fmt.Errorf("map: originate %s: %w", sc.Name, err)
		}

		conn, err := gateway.AcceptOutbound(ln, *acceptTimeout)
		if err != nil {
			cancel()
			ln.Close()
			return fmt.Errorf("map: %s: call did not connect: %w", sc.Name, err)
		}

		opts := gateway.AudioSocketOptions{
			LogDir:   *logdir,
			Control:  ami,
			Channels: ami,
		}
		uuid, runErr := gateway.RunOutboundScenario(ctx, conn, opts, sc)
		conn.Close()
		ln.Close()
		cancel()

		if runErr != nil {
			log.Printf("ivrvg map: %s: %v", sc.Name, runErr)
		}

		logPath := filepath.Join(*logdir, uuid+".jsonl")
		events, err := eventlog.Read(logPath)
		if err != nil {
			log.Printf("ivrvg map: %s: could not read log %s: %v", sc.Name, logPath, err)
			continue
		}
		allLogs = append(allLogs, events)
		log.Printf("ivrvg map: %s complete (%d events)", sc.Name, len(events))
	}

	if len(allLogs) == 0 {
		return fmt.Errorf("map: no scenario runs produced event logs")
	}

	target := scenarios[0].Target
	m := flowmap.Build(target, allLogs)
	if err := flowmap.Save(*outFile, m); err != nil {
		return fmt.Errorf("map: save: %w", err)
	}
	fmt.Printf("flow map: %d nodes, %d edges -> %s\n", len(m.Nodes), len(m.Edges), *outFile)
	return nil
}

func cmdDiff(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("diff: two map files are required: ivrvg diff <before.json> <after.json>")
	}
	before, err := flowmap.Load(args[0])
	if err != nil {
		return err
	}
	after, err := flowmap.Load(args[1])
	if err != nil {
		return err
	}

	changes := flowmap.Diff(before, after)
	if len(changes) == 0 {
		fmt.Println("no changes")
		return nil
	}

	data, err := json.MarshalIndent(changes, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	fmt.Printf("\n%d change(s)\n", len(changes))
	os.Exit(1)
	return nil
}

func outboundChannel(template, number string) (string, error) {
	if strings.Count(template, "{number}") != 1 {
		return "", fmt.Errorf("dial: -channel must contain exactly one {number} placeholder")
	}
	if number == "" || strings.ContainsAny(number+template, "\r\n") {
		return "", fmt.Errorf("dial: number and -channel must be non-empty and contain no line breaks")
	}
	return strings.Replace(template, "{number}", number, 1), nil
}

func newCallID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	encoded := hex.EncodeToString(id[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
