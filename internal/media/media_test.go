package media

import "testing"

func TestFrameGeometry(t *testing.T) {
	if SamplesPerFrame != 160 {
		t.Fatalf("SamplesPerFrame = %d, want 160", SamplesPerFrame)
	}
	if FrameBytes != 320 {
		t.Fatalf("FrameBytes = %d, want 320", FrameBytes)
	}
}

func TestMulawKnownValues(t *testing.T) {
	// Zero maps to 0xFF and back to zero — the canonical mu-law anchor.
	if got := MulawEncode([]int16{0})[0]; got != 0xFF {
		t.Fatalf("encode(0) = 0x%02X, want 0xFF", got)
	}
	if got := MulawDecode([]byte{0xFF})[0]; got != 0 {
		t.Fatalf("decode(0xFF) = %d, want 0", got)
	}
}

func TestMulawRoundTripSignAndRange(t *testing.T) {
	cases := []int16{0, 1, -1, 100, -100, 1000, -1000, 10000, -10000, 32000, -32000}
	for _, want := range cases {
		got := MulawDecode(MulawEncode([]int16{want}))[0]
		if want == 0 {
			if got != 0 {
				t.Fatalf("roundtrip(0) = %d", got)
			}
			continue
		}
		if (got > 0) != (want > 0) {
			t.Fatalf("roundtrip(%d) = %d changed sign", want, got)
		}
		// Mu-law is lossy; require the error to stay within a modest fraction.
		diff := int(got) - int(want)
		if diff < 0 {
			diff = -diff
		}
		if diff > int(want)/8+16 {
			t.Fatalf("roundtrip(%d) = %d, error too large", want, got)
		}
	}
}

func TestSilenceFrames(t *testing.T) {
	frames := SilenceFrames(3)
	if len(frames) != 3 {
		t.Fatalf("frames = %d, want 3", len(frames))
	}
	for i, f := range frames {
		if len(f) != FrameBytes {
			t.Fatalf("frame %d len = %d, want %d", i, len(f), FrameBytes)
		}
	}
}
