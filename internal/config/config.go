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
