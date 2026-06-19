package fasttrack

import (
	"context"
	"log"
	"time"
)

const defaultTimeout = 8 * time.Second

// STTClient transcribes PCM audio to text.
type STTClient interface {
	Transcribe(ctx context.Context, audio []byte) (string, error)
	Enabled() bool
}

// LLMClient generates a conversational reply from transcribed text.
type LLMClient interface {
	Complete(ctx context.Context, userText string) (string, error)
	Enabled() bool
}

// TTSClient synthesizes text to PCM audio.
type TTSClient interface {
	Synthesize(ctx context.Context, text string) ([]byte, error)
	Enabled() bool
}

// AudioSender delivers the TTS response back to the originating WebSocket session.
type AudioSender interface {
	SendAudioToSession(sessionID string, audio []byte) error
}

// Service runs the fast track pipeline: STT → LLM → TTS → WebSocket.
type Service struct {
	stt     STTClient
	llm     LLMClient
	tts     TTSClient
	sender  AudioSender
	timeout time.Duration
}

func NewService(stt STTClient, llm LLMClient, tts TTSClient, sender AudioSender, timeout time.Duration) *Service {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Service{stt: stt, llm: llm, tts: tts, sender: sender, timeout: timeout}
}

// Enabled reports whether all three services are configured.
func (s *Service) Enabled() bool {
	return s.stt.Enabled() && s.llm.Enabled() && s.tts.Enabled()
}

// Run executes STT → LLM → TTS and sends the audio response to the session.
// Intended to run in a goroutine; logs errors instead of returning them.
func (s *Service) Run(ctx context.Context, sessionID string, audio []byte) {
	if !s.Enabled() {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	start := time.Now()

	text, err := s.stt.Transcribe(ctx, audio)
	if err != nil {
		log.Printf("[%s] fast track STT failed: %v", sessionID, err)
		return
	}
	if text == "" {
		log.Printf("[%s] fast track STT returned empty text, skipping", sessionID)
		return
	}
	log.Printf("[%s] fast track STT | %q elapsed=%dms", sessionID, text, time.Since(start).Milliseconds())

	reply, err := s.llm.Complete(ctx, text)
	if err != nil {
		log.Printf("[%s] fast track LLM failed: %v", sessionID, err)
		return
	}
	if reply == "" {
		log.Printf("[%s] fast track LLM returned empty reply, skipping", sessionID)
		return
	}
	log.Printf("[%s] fast track LLM | %q elapsed=%dms", sessionID, reply, time.Since(start).Milliseconds())

	audioOut, err := s.tts.Synthesize(ctx, reply)
	if err != nil {
		log.Printf("[%s] fast track TTS failed: %v", sessionID, err)
		return
	}
	if len(audioOut) == 0 {
		log.Printf("[%s] fast track TTS returned empty audio, skipping", sessionID)
		return
	}
	log.Printf("[%s] fast track TTS | %d bytes elapsed=%dms", sessionID, len(audioOut), time.Since(start).Milliseconds())

	log.Printf("[%s] fast track sending audio to session...", sessionID)
	if err := s.sender.SendAudioToSession(sessionID, audioOut); err != nil {
		log.Printf("[%s] fast track send failed: %v", sessionID, err)
		return
	}

	log.Printf("[%s] fast track done | tts=%d bytes total=%dms", sessionID, len(audioOut), time.Since(start).Milliseconds())
}
