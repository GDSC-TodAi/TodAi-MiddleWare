package websocket

import (
	"sync"

	gws "github.com/gorilla/websocket"
)

// Session holds state for a single WebSocket client connection.
type Session struct {
	ID   string
	conn *gws.Conn
	mu   sync.Mutex // guards concurrent writes to conn
	done chan struct{}
}

func newSession(id string, conn *gws.Conn) *Session {
	return &Session{
		ID:   id,
		conn: conn,
		done: make(chan struct{}),
	}
}

// Done returns a channel closed when the session ends.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Send writes a binary message to the WebSocket connection.
func (s *Session) Send(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteMessage(gws.BinaryMessage, data)
}
