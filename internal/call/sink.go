package call

import (
	"sync"

	"github.com/swiftkimani/ivr-voice-gateway/internal/wire"
)

// MemorySink records everything the engine emits. It is the Sink used by tests
// and by replay, where the "caller" is a recording rather than a live socket.
type MemorySink struct {
	mu     sync.Mutex
	msgs   []wire.Message
	audio  [][]byte
	audioN int
}

// Message records an outbound text message.
func (m *MemorySink) Message(msg wire.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, msg)
	return nil
}

// Audio records an outbound PCM frame (copied, so callers may reuse buffers).
func (m *MemorySink) Audio(pcm []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(pcm))
	copy(cp, pcm)
	m.audio = append(m.audio, cp)
	m.audioN += len(pcm)
	return nil
}

// Messages returns a copy of the recorded text messages.
func (m *MemorySink) Messages() []wire.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]wire.Message, len(m.msgs))
	copy(out, m.msgs)
	return out
}

// AudioBytes is the total number of audio bytes emitted.
func (m *MemorySink) AudioBytes() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.audioN
}

// Reset clears everything recorded so far.
func (m *MemorySink) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = nil
	m.audio = nil
	m.audioN = 0
}
