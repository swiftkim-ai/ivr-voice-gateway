package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultDeepgramEndpoint = "https://api.deepgram.com/v1/listen"

// DeepgramConfig configures the Deepgram STT provider.
type DeepgramConfig struct {
	APIKey   string
	Model    string // e.g. "nova-2", "nova-2-phonecall"; defaults to "nova-2"
	Language string // e.g. "en"; defaults to "en"
	Endpoint string // defaults to Deepgram's API
}

// Deepgram implements Transcriber via the Deepgram pre-recorded audio API.
// The call engine feeds complete utterances (bounded by VAD silence), so each
// call sends one chunk of PCM16 audio and gets back a final transcription.
type Deepgram struct {
	apiKey   string
	model    string
	language string
	endpoint string
	client   *http.Client
}

// NewDeepgram returns a Deepgram transcriber.
func NewDeepgram(cfg DeepgramConfig) *Deepgram {
	model := cfg.Model
	if model == "" {
		model = "nova-2"
	}
	language := cfg.Language
	if language == "" {
		language = "en"
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultDeepgramEndpoint
	}
	return &Deepgram{
		apiKey:   cfg.APIKey,
		model:    model,
		language: language,
		endpoint: endpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Deepgram API response types.

type dgResponse struct {
	Results dgResults `json:"results"`
	Error   string    `json:"err_msg,omitempty"`
}

type dgResults struct {
	Channels []dgChannel `json:"channels"`
}

type dgChannel struct {
	Alternatives []dgAlternative `json:"alternatives"`
}

type dgAlternative struct {
	Transcript string  `json:"transcript"`
	Confidence float64 `json:"confidence"`
}

// Transcribe sends PCM16 8 kHz mono audio to Deepgram and returns the
// transcription. The audio is sent as raw linear16 with the encoding,
// sample rate, and channels specified in query parameters.
func (d *Deepgram) Transcribe(ctx context.Context, pcm []byte) (Result, error) {
	if len(pcm) == 0 {
		return Result{Final: true}, nil
	}

	url := fmt.Sprintf("%s?model=%s&language=%s&encoding=linear16&sample_rate=8000&channels=1",
		d.endpoint, d.model, d.language)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(pcm))
	if err != nil {
		return Result{}, fmt.Errorf("deepgram: build request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+d.apiKey)
	req.Header.Set("Content-Type", "audio/raw")

	resp, err := d.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("deepgram: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("deepgram: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("deepgram: API %d: %s", resp.StatusCode, body)
	}

	var dgResp dgResponse
	if err := json.Unmarshal(body, &dgResp); err != nil {
		return Result{}, fmt.Errorf("deepgram: parse response: %w", err)
	}
	if dgResp.Error != "" {
		return Result{}, fmt.Errorf("deepgram: %s", dgResp.Error)
	}

	if len(dgResp.Results.Channels) == 0 || len(dgResp.Results.Channels[0].Alternatives) == 0 {
		return Result{Final: true}, nil
	}

	alt := dgResp.Results.Channels[0].Alternatives[0]
	return Result{
		Text:  alt.Transcript,
		Final: true,
		Lang:  d.language,
		Score: alt.Confidence,
	}, nil
}
