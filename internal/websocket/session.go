package websocket

import gws "github.com/gorilla/websocket"

// Session holds state for a single WebSocket client connection.
type Session struct {
	ID   string
	conn *gws.Conn

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
