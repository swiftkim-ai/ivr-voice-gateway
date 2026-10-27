package media

import (
	"encoding/binary"
	"math"
	"testing"
)

// toneFrame generates a 20 ms frame of a sine wave at the given amplitude.
func toneFrame(amplitude int16) []byte {
	frame := make([]byte, FrameBytes)
	for i := 0; i < SamplesPerFrame; i++ {
		t := float64(i) / float64(SampleRate)
		s := int16(float64(amplitude) * math.Sin(2*math.Pi*440*t))
		binary.LittleEndian.PutUint16(frame[i*2:], uint16(s))
	}
	return frame
}

func silenceFrame() []byte {
	return make([]byte, FrameBytes)
}

func TestVADSpeechThenSilence(t *testing.T) {
	v := &VAD{}
	loud := toneFrame(8000)

	// Feed 10 frames of speech — should not trigger yet.
	for i := 0; i < 10; i++ {
		if v.Feed(loud) {
			t.Fatalf("triggered during speech at frame %d", i)
		}
	}
	if !v.Speaking() {
		t.Error("expected speaking=true during speech")
	}

	// Feed silence until it triggers.
	triggered := false
	for i := 0; i < 20; i++ {
		if v.Feed(silenceFrame()) {
			triggered = true
			break
		}
	}
	if !triggered {
		t.Fatal("VAD did not trigger after speech + silence")
	}

	utt := v.Utterance()
	if len(utt) == 0 {
		t.Error("utterance is empty")
	}
	// 10 speech frames + ~15 silence frames = ~25 frames
	expectedMin := 10 * FrameBytes
	if len(utt) < expectedMin {
		t.Errorf("utterance = %d bytes, want at least %d", len(utt), expectedMin)
	}

	// After Utterance(), the VAD should be idle.
	if v.Speaking() {
		t.Error("expected speaking=false after Utterance()")
	}
}

func TestVADPureSilence(t *testing.T) {
	v := &VAD{}
	for i := 0; i < 100; i++ {
		if v.Feed(silenceFrame()) {
			t.Fatal("triggered on pure silence")
		}
	}
}

func TestVADReset(t *testing.T) {
	v := &VAD{}
	v.Feed(toneFrame(8000))
	v.Reset()
	if v.Speaking() {
		t.Error("expected idle after Reset()")
	}
	utt := v.Utterance()
	if len(utt) != 0 {
		t.Error("expected empty after Reset()")
	}
}

func TestVADCustomThreshold(t *testing.T) {
	v := &VAD{Threshold: 10000}
	// Quiet tone that's above the default threshold but below 10000.
	quiet := toneFrame(2000)

	for i := 0; i < 20; i++ {
		if v.Feed(quiet) {
			t.Fatal("triggered with quiet audio below custom threshold")
		}
	}
	if v.Speaking() {
		t.Error("should not be speaking below custom threshold")
	}
}

func TestRMS(t *testing.T) {
	// Silence should have RMS of 0.
	s := silenceFrame()
	if got := rms(s); got != 0 {
		t.Errorf("silence rms = %f", got)
	}

	// A constant signal of 1000 should have RMS of 1000.
	frame := make([]byte, FrameBytes)
	for i := 0; i < SamplesPerFrame; i++ {
		binary.LittleEndian.PutUint16(frame[i*2:], uint16(int16(1000)))
	}
	got := rms(frame)
	if math.Abs(got-1000) > 1 {
		t.Errorf("constant 1000 rms = %f, want 1000", got)
	}
}
