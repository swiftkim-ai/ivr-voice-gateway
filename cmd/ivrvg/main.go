// Command ivrvg is the IVR Voice Gateway: it serves calls over the v1 contract,
// replays recorded calls deterministically, and writes sample recordings.
//
//	ivrvg serve  [-addr :8099] [-logdir data/calls] [-audiosocket :8090] [-ami host:5038]
//	ivrvg dial   [-ami host:5038] [-ami-user ivrvg] [-ami-context default] [-exten ...] [-app ...] [-callerid ...] <number>
//	ivrvg replay <call.jsonl>
//	ivrvg sample [-out testdata/calls/example.jsonl]
//	ivrvg version
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/call"
	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
	"github.com/swiftkimani/ivr-voice-gateway/internal/gateway"
	"github.com/swiftkimani/ivr-voice-gateway/internal/replay"
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
  dial    [-ami host:5038] [-ami-user ivrvg] [-ami-context default] [-exten ...] [-app ...] [-callerid ...] <number>
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

// dialApp is the default dialplan context used for the Local channel.
const dialApp = "ivrvg-out"

func cmdDial(args []string) error {
	fs := flag.NewFlagSet("dial", flag.ExitOnError)
	amiAddr := fs.String("ami", "", "Asterisk manager address host:port")
	amiUser := fs.String("ami-user", "ivrvg", "Asterisk manager username")
	amiContext := fs.String("ami-context", "default", "dialplan context that route and transfer redirect into")
	exten := fs.String("exten", "", "extension to dial through (defaults to the number)")
	app := fs.String("app", dialApp, "dialplan application/context to send the call to")
	callerID := fs.String("callerid", "ivrvg", "caller id to present")
	_ = fs.Parse(args)
	if len(fs.Args()) < 1 {
		return fmt.Errorf("dial: a number is required")
	}
	if *amiAddr == "" {
		return fmt.Errorf("dial: -ami is required")
	}
	ami, err := dialManager(*amiAddr, *amiUser, *amiContext)
	if err != nil {
		return err
	}
	defer ami.Close()

	number := fs.Args()[0]
	dest := *exten
	if dest == "" {
		dest = number
	}
	callID := fmt.Sprintf("out-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ami.Originate(ctx, control.Originate{
		Channel:   "Local/" + dest + "@" + *app + "/n",
		Context:   *amiContext,
		Exten:     dest,
		Priority:  1,
		CallerID:  *callerID,
		Timeout:   30000,
		Async:     true,
		Variables: map[string]string{"IVRVG_CALL_ID": callID, "IVRVG_HINT": number},
	}); err != nil {
		return fmt.Errorf("dial: originate %s: %w", number, err)
	}
	fmt.Printf("origination accepted for %s (call %s)\n", number, callID)
	return nil
}
