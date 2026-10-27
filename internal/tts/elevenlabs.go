package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultElevenLabsEndpoint = "https://api.elevenlabs.io/v1/text-to-speech"

// ElevenLabsConfig configures the ElevenLabs TTS provider.
type ElevenLabsConfig struct {
	APIKey   string
	VoiceID  string // required; the voice to synthesize with
	ModelID  string // e.g. "eleven_turbo_v2_5"; defaults to "eleven_turbo_v2_5"
	Endpoint string // defaults to ElevenLabs API
}

// ElevenLabs implements Synthesizer via the ElevenLabs text-to-speech API.
// Returns PCM16 signed little-endian mono at 16 kHz (the lowest rate
// ElevenLabs serves); the Speaker resamples to 8 kHz for the phone path.
type ElevenLabs struct {
	apiKey   string
	voiceID  string
	modelID  string
	endpoint string
	client   *http.Client
}

// NewElevenLabs returns an ElevenLabs synthesizer.
func NewElevenLabs(cfg ElevenLabsConfig) *ElevenLabs {
	modelID := cfg.ModelID
	if modelID == "" {
		modelID = "eleven_turbo_v2_5"
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultElevenLabsEndpoint
	}
	return &ElevenLabs{
		apiKey:   cfg.APIKey,
		voiceID:  cfg.VoiceID,
		modelID:  modelID,
		endpoint: endpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

type elRequest struct {
	Text          string           `json:"text"`
	ModelID       string           `json:"model_id"`
	OutputFormat  string           `json:"output_format"`
	VoiceSettings *elVoiceSettings `json:"voice_settings,omitempty"`
}

type elVoiceSettings struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
}

// Synthesize sends text to ElevenLabs and returns raw PCM16 audio at 16 kHz.
func (e *ElevenLabs) Synthesize(ctx context.Context, text string) ([]byte, int, error) {
	if text == "" {
		return nil, 0, nil
	}

	url := fmt.Sprintf("%s/%s", e.endpoint, e.voiceID)

	payload := elRequest{
		Text:         text,
		ModelID:      e.modelID,
		OutputFormat: "pcm_16000",
		VoiceSettings: &elVoiceSettings{
			Stability:       0.5,
			SimilarityBoost: 0.75,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("elevenlabs: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("elevenlabs: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("elevenlabs: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("elevenlabs: API %d: %s", resp.StatusCode, errBody)
	}

	pcm, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("elevenlabs: read audio: %w", err)
	}

	return pcm, 16000, nil
}
