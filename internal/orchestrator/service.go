package orchestrator

import (
	"bytes"
	"context"
	"log"
	"sync"

	"github.com/Hyuk-II/todai-middleware/internal/vad"
)

const rmsLogInterval = 20 // 청크 N개마다 RMS 로그 출력 (캘리브레이션용)

// UtterancePublisher handles a complete spoken utterance for background analysis.
type UtterancePublisher interface {
	PublishUtterance(ctx context.Context, sessionID string, audioData []byte) error
}

// Service buffers audio per session, runs VAD, and fires the slow track once per utterance.
type Service struct {
	mu        sync.Mutex
	sessions  map[string]*sessionState
	publisher UtterancePublisher
}

type sessionState struct {
	vad       *vad.Detector
	buf       bytes.Buffer
	chunkCount int
}

func NewService(publisher UtterancePublisher) *Service {
	return &Service{
		sessions:  make(map[string]*sessionState),
		publisher: publisher,
	}
}

// HandleAudioChunk implements websocket.AudioChunkHandler.
// Each chunk is buffered and passed to VAD. On utterance end, the full audio is published.
func (s *Service) HandleAudioChunk(ctx context.Context, sessionID string, audioData []byte) {
	s.mu.Lock()
	state := s.getOrCreate(sessionID)
	s.mu.Unlock()

	// sessionState is accessed only by this session's goroutine — no lock needed here.
	state.chunkCount++
	rms := vad.RMS(audioData)
	if state.chunkCount%rmsLogInterval == 0 {
		log.Printf("[%s] VAD rms=%.1f", sessionID, rms)
	}

	state.buf.Write(audioData)
	if !state.vad.Observe(audioData) {
		return
	}

	utterance := append([]byte(nil), state.buf.Bytes()...)
	state.buf.Reset()

	log.Printf("[%s] utterance end detected | %d bytes → slow track", sessionID, len(utterance))
	if s.publisher != nil {
		go func() {
			if err := s.publisher.PublishUtterance(
				context.WithoutCancel(ctx),
				sessionID,
				utterance,
			); err != nil {
				log.Printf("[%s] slow track failed: %v", sessionID, err)
			}
		}()
	}
}

// OnSessionClose removes per-session state when the WebSocket connection closes.
func (s *Service) OnSessionClose(sessionID string) {
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
}

func (s *Service) getOrCreate(sessionID string) *sessionState {
	if state, ok := s.sessions[sessionID]; ok {
		return state
	}
	state := &sessionState{vad: vad.NewDetector()}
	s.sessions[sessionID] = state
	return state
}
