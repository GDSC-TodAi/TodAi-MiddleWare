package config

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultPort                  = "8080"
	defaultRabbitMQURL           = "amqp://guest:guest@localhost:5672/"
	defaultRabbitMQEmotionQueue  = "todai.worker.emotion"
	defaultRabbitMQSTTQueue      = "todai.worker.stt"
	defaultRabbitMQReplyQueue    = "todai.reply"
	defaultPublishTimeoutSeconds = 3
	defaultBackendTimeoutSeconds = 3
	defaultSTTTimeoutSeconds     = 5
	defaultLLMTimeoutSeconds     = 10
	defaultTTSTimeoutSeconds     = 5
	defaultADKTimeoutSeconds     = 10
	defaultFastTrackTimeoutSeconds = 8
	defaultLLMModel              = "llama3.2"
)

type Config struct {
	Port                   string
	RabbitMQURL            string
	RabbitMQEmotionQ       string
	RabbitMQSTTQ           string
	RabbitMQReplyQ         string
	RabbitMQPublishTimeout time.Duration
	BackendBaseURL         string
	BackendRequestTimeout  time.Duration

	// Fast Track
	STTBaseURL          string
	STTTimeout          time.Duration
	LLMBaseURL          string
	LLMTimeout          time.Duration
	LLMModel            string
	TTSBaseURL          string
	TTSTimeout          time.Duration
	FastTrackTimeout    time.Duration

	// ADK (Slow Track finalization)
	ADKBaseURL string
	ADKTimeout time.Duration
}

func Load() *Config {
	return &Config{
		Port:                   envOrDefault("PORT", defaultPort),
		RabbitMQURL:            envOrDefault("RABBITMQ_URL", defaultRabbitMQURL),
		RabbitMQEmotionQ:       envOrDefault("RABBITMQ_EMOTION_QUEUE", defaultRabbitMQEmotionQueue),
		RabbitMQSTTQ:           envOrDefault("RABBITMQ_STT_QUEUE", defaultRabbitMQSTTQueue),
		RabbitMQReplyQ:         envOrDefault("RABBITMQ_REPLY_QUEUE", defaultRabbitMQReplyQueue),
		RabbitMQPublishTimeout: publishTimeout(),
		BackendBaseURL:         os.Getenv("BACKEND_BASE_URL"),
		BackendRequestTimeout:  durationSeconds("BACKEND_REQUEST_TIMEOUT_SECONDS", defaultBackendTimeoutSeconds),

		STTBaseURL:       os.Getenv("STT_BASE_URL"),
		STTTimeout:       durationSeconds("STT_TIMEOUT_SECONDS", defaultSTTTimeoutSeconds),
		LLMBaseURL:       os.Getenv("LLM_BASE_URL"),
		LLMTimeout:       durationSeconds("LLM_TIMEOUT_SECONDS", defaultLLMTimeoutSeconds),
		LLMModel:         envOrDefault("LLM_MODEL", defaultLLMModel),
		TTSBaseURL:       os.Getenv("TTS_BASE_URL"),
		TTSTimeout:       durationSeconds("TTS_TIMEOUT_SECONDS", defaultTTSTimeoutSeconds),
		FastTrackTimeout: durationSeconds("FAST_TRACK_TIMEOUT_SECONDS", defaultFastTrackTimeoutSeconds),

		ADKBaseURL: os.Getenv("ADK_BASE_URL"),
		ADKTimeout: durationSeconds("ADK_TIMEOUT_SECONDS", defaultADKTimeoutSeconds),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func publishTimeout() time.Duration {
	return durationSeconds("RABBITMQ_PUBLISH_TIMEOUT_SECONDS", defaultPublishTimeoutSeconds)
}

func durationSeconds(key string, fallbackSeconds int) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return time.Duration(fallbackSeconds) * time.Second
	}

	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return time.Duration(fallbackSeconds) * time.Second
	}
	return time.Duration(seconds) * time.Second
}
