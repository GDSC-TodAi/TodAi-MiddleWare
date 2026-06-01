package websocket

import (
	"sync"

	gws "github.com/gorilla/websocket"
)

// Session holds state for a single WebSocket client connection.
type Session struct {
	ID   string
	conn *gws.Conn

	mu     sync.Mutex
	chunks [][]byte // raw audio chunks, drained by VAD in step 2
	done   chan struct{}
}

func newSession(id string, conn *gws.Conn) *Session {
	return &Session{
		ID:     id,
		conn:   conn,
		chunks: make([][]byte, 0, 64),
		done:   make(chan struct{}),
	}
}

func (s *Session) appendChunk(chunk []byte) {
	cp := make([]byte, len(chunk))
	copy(cp, chunk)

	s.mu.Lock()
	s.chunks = append(s.chunks, cp)
	s.mu.Unlock()
}

// DrainChunks returns all buffered chunks and resets the buffer.
// Called by VAD (step 2) when a utterance boundary is detected.
func (s *Session) DrainChunks() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.chunks
	s.chunks = make([][]byte, 0, 64)
	return out
}

// TotalBuffered returns the total bytes currently buffered.
func (s *Session) TotalBuffered() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, c := range s.chunks {
		total += len(c)
	}
	return total
}

// Done returns a channel closed when the session ends.
func (s *Session) Done() <-chan struct{} {
	return s.done
}
