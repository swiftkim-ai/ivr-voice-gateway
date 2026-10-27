package tts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestElevenLabsSynthesize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("xi-api-key"); got != "test-key" {
			t.Errorf("api key = %q", got)
		}

		if !strings.HasSuffix(r.URL.Path, "/voice-123") {
			t.Errorf("path = %q, want .../voice-123", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var req elRequest
		json.Unmarshal(body, &req)

		if req.Text != "Hello caller." {
			t.Errorf("text = %q", req.Text)
		}
		if req.OutputFormat != "pcm_16000" {
			t.Errorf("format = %q", req.OutputFormat)
		}
		if req.ModelID != "eleven_turbo_v2_5" {
			t.Errorf("model = %q", req.ModelID)
		}

		// Return 320 bytes = 10 ms of 16 kHz PCM16 mono
		w.Header().Set("Content-Type", "audio/raw")
		w.Write(make([]byte, 320))
	}))
	defer srv.Close()

	e := NewElevenLabs(ElevenLabsConfig{
		APIKey:   "test-key",
		VoiceID:  "voice-123",
		Endpoint: srv.URL,
	})

	pcm, rate, err := e.Synthesize(context.Background(), "Hello caller.")
	if err != nil {
		t.Fatal(err)
	}
	if rate != 16000 {
		t.Errorf("rate = %d", rate)
	}
	if len(pcm) != 320 {
		t.Errorf("pcm bytes = %d", len(pcm))
	}
}

func TestElevenLabsEmptyText(t *testing.T) {
	e := NewElevenLabs(ElevenLabsConfig{APIKey: "k", VoiceID: "v", Endpoint: "http://unused"})
	pcm, rate, err := e.Synthesize(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if pcm != nil || rate != 0 {
		t.Errorf("expected nil/0 for empty text, got %d bytes rate %d", len(pcm), rate)
	}
}

func TestElevenLabsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"detail":{"status":"invalid_api_key"}}`))
	}))
	defer srv.Close()

	e := NewElevenLabs(ElevenLabsConfig{APIKey: "bad", VoiceID: "v", Endpoint: srv.URL})
	_, _, err := e.Synthesize(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestElevenLabsSpeaker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 640 bytes = 20 ms at 16 kHz = will become 10 ms at 8 kHz = 1 partial frame
		// Actually: 640 bytes at 16 kHz = 20 ms. Resampled to 8 kHz = 320 bytes = 1 full frame.
		w.Write(make([]byte, 640))
	}))
	defer srv.Close()

	e := NewElevenLabs(ElevenLabsConfig{APIKey: "k", VoiceID: "v", Endpoint: srv.URL})
	spk := Speaker{Synth: e}

	frames := spk.Frames("test")
	if len(frames) == 0 {
		t.Error("expected at least one frame")
	}
	for i, f := range frames {
		if len(f) != 320 {
			t.Errorf("frame %d: %d bytes, want 320", i, len(f))
		}
	}
}

func TestElevenLabsCustomModel(t *testing.T) {
	var capturedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req elRequest
		json.Unmarshal(body, &req)
		capturedModel = req.ModelID
		w.Write(make([]byte, 32))
	}))
	defer srv.Close()

	e := NewElevenLabs(ElevenLabsConfig{
		APIKey:   "k",
		VoiceID:  "v",
		ModelID:  "eleven_multilingual_v2",
		Endpoint: srv.URL,
	})
	e.Synthesize(context.Background(), "test")

	if capturedModel != "eleven_multilingual_v2" {
		t.Errorf("model = %q", capturedModel)
	}
}
