package stt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeepgramTranscribe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token test-key" {
			t.Errorf("auth = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "audio/raw" {
			t.Errorf("content-type = %q", got)
		}

		q := r.URL.Query()
		if q.Get("encoding") != "linear16" {
			t.Errorf("encoding = %q", q.Get("encoding"))
		}
		if q.Get("sample_rate") != "8000" {
			t.Errorf("sample_rate = %q", q.Get("sample_rate"))
		}
		if q.Get("model") != "nova-2-phonecall" {
			t.Errorf("model = %q", q.Get("model"))
		}

		audio, _ := io.ReadAll(r.Body)
		if len(audio) != 160 {
			t.Errorf("audio bytes = %d, want 160", len(audio))
		}

		json.NewEncoder(w).Encode(dgResponse{
			Results: dgResults{
				Channels: []dgChannel{{
					Alternatives: []dgAlternative{{
						Transcript: "hello world",
						Confidence: 0.98,
					}},
				}},
			},
		})
	}))
	defer srv.Close()

	d := NewDeepgram(DeepgramConfig{
		APIKey:   "test-key",
		Model:    "nova-2-phonecall",
		Endpoint: srv.URL,
	})

	pcm := make([]byte, 160)
	result, err := d.Transcribe(context.Background(), pcm)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hello world" {
		t.Errorf("text = %q", result.Text)
	}
	if !result.Final {
		t.Error("expected final=true")
	}
	if result.Score != 0.98 {
		t.Errorf("score = %f", result.Score)
	}
}

func TestDeepgramEmptyAudio(t *testing.T) {
	d := NewDeepgram(DeepgramConfig{APIKey: "k", Endpoint: "http://unused"})
	result, err := d.Transcribe(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "" {
		t.Errorf("text = %q for empty audio", result.Text)
	}
	if !result.Final {
		t.Error("expected final=true")
	}
}

func TestDeepgramAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"err_msg":"invalid credentials"}`))
	}))
	defer srv.Close()

	d := NewDeepgram(DeepgramConfig{APIKey: "bad", Endpoint: srv.URL})
	_, err := d.Transcribe(context.Background(), make([]byte, 160))
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestDeepgramEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(dgResponse{
			Results: dgResults{Channels: []dgChannel{}},
		})
	}))
	defer srv.Close()

	d := NewDeepgram(DeepgramConfig{APIKey: "k", Endpoint: srv.URL})
	result, err := d.Transcribe(context.Background(), make([]byte, 160))
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "" {
		t.Errorf("text = %q for empty result", result.Text)
	}
}

func TestDeepgramDefaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("model") != "nova-2" {
			t.Errorf("default model = %q", q.Get("model"))
		}
		if q.Get("language") != "en" {
			t.Errorf("default language = %q", q.Get("language"))
		}
		json.NewEncoder(w).Encode(dgResponse{
			Results: dgResults{Channels: []dgChannel{{
				Alternatives: []dgAlternative{{Transcript: "ok"}},
			}}},
		})
	}))
	defer srv.Close()

	d := NewDeepgram(DeepgramConfig{APIKey: "k", Endpoint: srv.URL})
	d.Transcribe(context.Background(), make([]byte, 16))
}
