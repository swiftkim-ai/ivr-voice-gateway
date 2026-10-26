package media

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestDTMFToneAllDigits(t *testing.T) {
	for _, d := range []byte("0123456789*#ABCD") {
		frames := DTMFTone(d)
		if len(frames) == 0 {
			t.Errorf("DTMFTone(%q) returned no frames", d)
			continue
		}
		for i, f := range frames {
			if len(f) != FrameBytes {
				t.Errorf("DTMFTone(%q) frame %d: len=%d, want %d", d, i, len(f), FrameBytes)
			}
		}
	}
}

func TestDTMFToneUnknownDigit(t *testing.T) {
	if frames := DTMFTone('X'); frames != nil {
		t.Errorf("DTMFTone('X') = %d frames, want nil", len(frames))
	}
}

func TestDTMFToneDuration(t *testing.T) {
	frames := DTMFTone('5')
	totalMS := len(frames) * FrameMS
	if totalMS != 200 {
		t.Errorf("DTMFTone('5') duration = %d ms, want 200 (100 tone + 100 gap)", totalMS)
	}
}

func TestDTMFToneHasSignal(t *testing.T) {
	frames := DTMFTone('1')
	var maxAbs int16
	for _, f := range frames[:len(frames)/2] {
		for off := 0; off+2 <= len(f); off += 2 {
			s := int16(binary.LittleEndian.Uint16(f[off:]))
			if s < 0 {
				s = -s
			}
			if s > maxAbs {
				maxAbs = s
			}
		}
	}
	if maxAbs < 1000 {
		t.Errorf("tone peak = %d, want at least 1000", maxAbs)
	}
}

func TestDTMFToneFrequencies(t *testing.T) {
	frames := DTMFTone('5')
	toneSamples := SampleRate * dtmfToneMS / 1000
	pcm := make([]float64, toneSamples)
	idx := 0
	for _, f := range frames {
		for off := 0; off+2 <= len(f) && idx < toneSamples; off += 2 {
			pcm[idx] = float64(int16(binary.LittleEndian.Uint16(f[off:])))
			idx++
		}
	}

	freqs := dtmfFreqs['5']
	for _, freq := range freqs {
		power := goertzel(pcm, freq, SampleRate)
		if power < 0.1 {
			t.Errorf("expected strong power at %.0f Hz, got %f", freq, power)
		}
	}

	offFreq := 500.0
	power := goertzel(pcm, offFreq, SampleRate)
	if power > 0.01 {
		t.Errorf("unexpected power at %.0f Hz: %f", offFreq, power)
	}
}

func goertzel(samples []float64, freq float64, sampleRate int) float64 {
	n := len(samples)
	k := int(0.5 + float64(n)*freq/float64(sampleRate))
	w := 2.0 * math.Pi * float64(k) / float64(n)
	coeff := 2.0 * math.Cos(w)
	var s0, s1, s2 float64
	for _, x := range samples {
		s0 = x + coeff*s1 - s2
		s2 = s1
		s1 = s0
	}
	power := s1*s1 + s2*s2 - coeff*s1*s2
	var sumSq float64
	for _, x := range samples {
		sumSq += x * x
	}
	if sumSq == 0 {
		return 0
	}
	return power / sumSq
}
