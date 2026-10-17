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

// Code points taken from Asterisk's ulaw.c, the implementation this codec has
// to agree with on the wire (see encodeMulaw).
func TestMulawKnownValues(t *testing.T) {
	encoded := []struct {
		pcm  int16
		code byte
	}{
		{0, 0xFF}, // the canonical anchor
		{100, 0xF2},
		{1000, 0xCE},
		{10000, 0x9C},
		{32000, 0x80}, // top segment
		{-100, 0x72},
		{-1000, 0x4E},
		{-32000, 0x00},
	}
	for _, c := range encoded {
		if got := MulawEncode([]int16{c.pcm})[0]; got != c.code {
			t.Fatalf("encode(%d) = 0x%02X, want 0x%02X", c.pcm, got, c.code)
		}
	}

	decoded := []struct {
		code byte
		pcm  int16
	}{
		{0xFF, 0},
		{0x80, 32124}, // the largest magnitude mu-law can represent
		{0x00, -32124},
	}
	for _, c := range decoded {
		if got := MulawDecode([]byte{c.code})[0]; got != c.pcm {
			t.Fatalf("decode(0x%02X) = %d, want %d", c.code, got, c.pcm)
		}
	}
}

// Mu-law is a piecewise-logarithmic quantiser: eight segments, each carrying a
// 4-bit mantissa with an implicit leading bit. These properties hold across the
// whole int16 range and together pin the segment table, because a misplaced
// segment boundary wraps the mantissa and breaks every one of them.
func TestMulawQuantisesWholeRange(t *testing.T) {
	// |pcm| <= 3 sits below the first step; see TestMulawDeadZone.
	const deadZone = 3

	prev := int(MulawDecode(MulawEncode([]int16{-32768}))[0])
	codes := make(map[byte]bool, 256)

	for x := -32768; x <= 32767; x++ {
		code := MulawEncode([]int16{int16(x)})[0]
		codes[code] = true
		got := int(MulawDecode([]byte{code})[0])

		// Companding keeps the error proportional to magnitude, never absolute.
		if err, bound := abs(got-x), abs(x)/16+8; err > bound {
			t.Fatalf("roundtrip(%d) = %d: error %d exceeds %d", x, got, err, bound)
		}
		// Sign survives anything the first step can represent.
		if abs(x) > deadZone && (got > 0) != (x > 0) {
			t.Fatalf("roundtrip(%d) = %d changed sign", x, got)
		}
		// A quantiser is monotonic; a wrapped mantissa is not.
		if got < prev {
			t.Fatalf("roundtrip not monotonic at %d: %d then %d", x, prev, got)
		}
		prev = got
	}

	if len(codes) != 256 {
		t.Fatalf("reachable codes = %d, want all 256", len(codes))
	}
}

func TestMulawDeadZone(t *testing.T) {
	// The first non-zero level is 8, so quiet samples have nothing nearer than
	// zero to land on. Documented because it reads like a sign bug.
	for _, pcm := range []int16{-3, -1, 0, 1, 3} {
		if got := MulawDecode(MulawEncode([]int16{pcm}))[0]; got != 0 {
			t.Fatalf("roundtrip(%d) = %d, want 0", pcm, got)
		}
	}
	for _, pcm := range []int16{-4, 4} {
		if got := MulawDecode(MulawEncode([]int16{pcm}))[0]; got == 0 {
			t.Fatalf("roundtrip(%d) = 0, want the first non-zero level", pcm)
		}
	}
}

func TestMulawFrameRoundTrip(t *testing.T) {
	// The slice forms must preserve length and order over a whole 20 ms frame,
	// independently of the per-sample arithmetic above.
	pcm := make([]int16, SamplesPerFrame)
	for i := range pcm {
		pcm[i] = int16((i - SamplesPerFrame/2) * 200)
	}

	ulaw := MulawEncode(pcm)
	if len(ulaw) != SamplesPerFrame {
		t.Fatalf("encoded length = %d, want %d", len(ulaw), SamplesPerFrame)
	}
	back := MulawDecode(ulaw)
	if len(back) != len(pcm) {
		t.Fatalf("decoded length = %d, want %d", len(back), len(pcm))
	}
	for i, s := range pcm {
		if want := MulawDecode(MulawEncode([]int16{s}))[0]; back[i] != want {
			t.Fatalf("frame sample %d = %d, want %d", i, back[i], want)
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

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
