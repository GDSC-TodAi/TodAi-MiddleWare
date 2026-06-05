package websocket

import (
	"context"
	"log"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	gws "github.com/gorilla/websocket"
)

var upgrader = gws.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Allow all origins during development; restrict in production.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler manages WebSocket upgrades and active sessions.
type Handler struct {
	mu           sync.RWMutex
	sessions     map[string]*Session
	audioHandler AudioChunkHandler
}

// AudioChunkHandler receives binary audio without exposing processing policy to WebSocket.
type AudioChunkHandler interface {
	HandleAudioChunk(ctx context.Context, sessionID string, audioData []byte)
}

func NewHandler(audioHandler AudioChunkHandler) *Handler {
	return &Handler{
		sessions:     make(map[string]*Session),
		audioHandler: audioHandler,
	}
}

// ServeHTTP upgrades an HTTP request to a WebSocket connection.
func (h *Handler) ServeHTTP(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}

	sessionID := uuid.New().String()
	sess := newSession(sessionID, conn)

	h.mu.Lock()
	h.sessions[sessionID] = sess
	h.mu.Unlock()

	log.Printf("[%s] connected (active: %d)", sessionID, h.activeCount())

	go h.readLoop(sess)
}

func (h *Handler) readLoop(sess *Session) {
	defer func() {
		sess.conn.Close()
		close(sess.done)

		h.mu.Lock()
		delete(h.sessions, sess.ID)
		h.mu.Unlock()

		log.Printf("[%s] disconnected (active: %d)", sess.ID, h.activeCount())

		if h.audioHandler != nil {
			if closer, ok := h.audioHandler.(interface{ OnSessionClose(string) }); ok {
				closer.OnSessionClose(sess.ID)
			}
		}
	}()

	for {
		msgType, data, err := sess.conn.ReadMessage()
		if err != nil {
			if gws.IsUnexpectedCloseError(err, gws.CloseGoingAway, gws.CloseNormalClosure) {
				log.Printf("[%s] read error: %v", sess.ID, err)
			}
			return
		}

		if msgType != gws.BinaryMessage {
			log.Printf("[%s] unexpected message type %d, ignoring", sess.ID, msgType)
			continue
		}

		log.Printf("[%s] audio chunk received | %d bytes", sess.ID, len(data))
		if h.audioHandler != nil {
			h.audioHandler.HandleAudioChunk(context.Background(), sess.ID, data)
		}
	}
}

func (h *Handler) activeCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}
