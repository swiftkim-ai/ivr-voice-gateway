// Command ivrvg is the IVR Voice Gateway: it serves calls over the v1 contract,
// replays recorded calls deterministically, and writes sample recordings.
//
//	ivrvg serve  [-addr :8099] [-logdir data/calls]
//	ivrvg replay <call.jsonl>
//	ivrvg sample [-out testdata/calls/example.jsonl]
//	ivrvg version
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/call"
	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
	"github.com/swiftkimani/ivr-voice-gateway/internal/gateway"
	"github.com/swiftkimani/ivr-voice-gateway/internal/replay"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe(os.Args[2:])
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
  replay  <call.jsonl>
  sample  [-out testdata/calls/example.jsonl]
  version
`)
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8099", "listen address")
	logdir := fs.String("logdir", "data/calls", "directory for per-call event logs")
	_ = fs.Parse(args)

	h := gateway.Handler(gateway.Options{
		LogDir:  *logdir,
		Brain:   demoBrain(),
		Speaker: brain.Silence{},
	})
	log.Printf("ivrvg %s listening on %s (ws /v1/media, logs -> %s)", version, *addr, *logdir)
	return http.ListenAndServe(*addr, h)
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
		{Match: "1", Say: []string{"Connecting you to sales now."},
			Actions: []control.Action{{Kind: control.Transfer, Args: map[string]any{"dest": "200"}}}},
		{Match: "2", Say: []string{"Let me book that for you."}},
		{Match: "0", Say: []string{"I will put you through to an operator."},
			Actions: []control.Action{{Kind: control.Transfer, Args: map[string]any{"dest": "100"}}}},
		{Match: "*", Say: []string{"Hello, you have reached the gateway. Press 1 for sales, 2 to book, or 0 for an operator."}},
	}}
}
