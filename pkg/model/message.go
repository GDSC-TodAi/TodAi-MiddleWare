package model

import "encoding/json"

const (
	WorkerTypeEmotion = "emotion"
	WorkerTypeSTT     = "stt"

	WorkerStatusSuccess = "success"
	WorkerStatusFailed  = "failed"
)

// WorkerRequest is the message Go sends to Python workers via RabbitMQ.
type WorkerRequest struct {
	JobID         string `json:"job_id"`
	SessionID     string `json:"session_id"`
	ElderID       string `json:"elder_id"`
	CorrelationID string `json:"correlation_id"`
	WorkerType    string `json:"worker_type"`
	ReplyTo       string `json:"reply_to"`
	AudioData     []byte `json:"audio_data"` // PCM 16bit, 16kHz, Mono
	Timestamp     int64  `json:"timestamp"`
}

// WorkerResponse is the response contract for the RabbitMQ reply queue.
type WorkerResponse struct {
	JobID         string          `json:"job_id"`
	SessionID     string          `json:"session_id"`
	ElderID       string          `json:"elder_id"`
	CorrelationID string          `json:"correlation_id"`
	WorkerType    string          `json:"worker_type"`
	Status        string          `json:"status"`
	Result        json.RawMessage `json:"result,omitempty"`
	ErrorMessage  string          `json:"error_message,omitempty"`
	Timestamp     int64           `json:"timestamp"`
}

// WorkerReply is the message Python workers send back via the reply queue.
// Deprecated: use WorkerResponse for new worker integrations.
type WorkerReply struct {
	CorrelationID string      `json:"correlation_id"`
	WorkerType    string      `json:"worker_type"` // "emotion" | "stt"
	Status        string      `json:"status"`      // "ok" | "error" | "partial"
	Result        interface{} `json:"result"`
	ElapsedMs     int64       `json:"elapsed_ms"`
}

// EmotionResult is the output of Worker A (emotion analysis).
type EmotionResult struct {
	Sadness float64 `json:"sadness"`
	Anxiety float64 `json:"anxiety"`
	Neutral float64 `json:"neutral"`
	Joy     float64 `json:"joy"`
}

// STTResult is the output of Worker B (speech-to-text).
type STTResult struct {
	Text string `json:"text"`
}
