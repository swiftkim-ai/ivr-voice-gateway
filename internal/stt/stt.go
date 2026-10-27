// Package stt defines the speech-to-text interface and its recorded fixture
// implementation. A real provider (Whisper, Deepgram, Google STT) implements
// Transcriber; the Recorded implementation replays saved transcriptions for
// deterministic tests.
package stt

import "context"

// Result is one transcription.
type Result struct {
	Text  string  `json:"text"`
	Final bool    `json:"final"`
	Lang  string  `json:"lang,omitempty"`
	Score float64 `json:"score,omitempty"`
}

// Transcriber turns audio into text. Implementations are streaming in nature,
// but the call engine feeds complete utterances (bounded by VAD silence), so
// each call to Transcribe receives one chunk and returns a final result.
type Transcriber interface {
	Transcribe(ctx context.Context, pcm []byte) (Result, error)
}
