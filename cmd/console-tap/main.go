// Command console-tap connects to the operator console WebSocket and writes
// received events to a file, one JSON object per line. It runs until the
// timeout expires or a signal arrives. Used by the escalation integration test.
//
//	console-tap -url ws://127.0.0.1:8099/v1/console -out events.jsonl -timeout 30s
package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	wsURL := flag.String("url", "ws://127.0.0.1:8099/v1/console", "console WebSocket URL")
	outPath := flag.String("out", "console-events.jsonl", "output file for received events")
	timeout := flag.Duration("timeout", 30*time.Second, "how long to listen before exiting")
	flag.Parse()

	u, err := url.Parse(*wsURL)
	if err != nil {
		log.Fatalf("console-tap: bad url: %v", err)
	}

	f, err := os.Create(*outPath)
	if err != nil {
		log.Fatalf("console-tap: create %s: %v", *outPath, err)
	}
	defer f.Close()

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("console-tap: dial %s: %v", u, err)
	}
	defer conn.Close()

	fmt.Fprintf(os.Stderr, "console-tap: connected to %s, writing to %s\n", u, *outPath)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			f.Write(msg)
			f.Write([]byte("\n"))
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	timer := time.NewTimer(*timeout)
	defer timer.Stop()

	select {
	case <-done:
	case <-sig:
	case <-timer.C:
	}
	fmt.Fprintf(os.Stderr, "console-tap: done\n")
}
