package config

import (
	"testing"
	"time"
)

func TestLoadRabbitMQDefaults(t *testing.T) {
	t.Setenv("RABBITMQ_URL", "")
	t.Setenv("RABBITMQ_EMOTION_QUEUE", "")
	t.Setenv("RABBITMQ_STT_QUEUE", "")
	t.Setenv("RABBITMQ_REPLY_QUEUE", "")
	t.Setenv("RABBITMQ_PUBLISH_TIMEOUT_SECONDS", "")

	cfg := Load()

	if cfg.RabbitMQURL != defaultRabbitMQURL {
		t.Fatalf("RabbitMQURL = %q, want %q", cfg.RabbitMQURL, defaultRabbitMQURL)
	}
	if cfg.RabbitMQEmotionQ != defaultRabbitMQEmotionQueue {
		t.Fatalf("RabbitMQEmotionQ = %q, want %q", cfg.RabbitMQEmotionQ, defaultRabbitMQEmotionQueue)
	}
	if cfg.RabbitMQSTTQ != defaultRabbitMQSTTQueue {
		t.Fatalf("RabbitMQSTTQ = %q, want %q", cfg.RabbitMQSTTQ, defaultRabbitMQSTTQueue)
	}
	if cfg.RabbitMQReplyQ != defaultRabbitMQReplyQueue {
		t.Fatalf("RabbitMQReplyQ = %q, want %q", cfg.RabbitMQReplyQ, defaultRabbitMQReplyQueue)
	}
	if cfg.RabbitMQPublishTimeout != defaultPublishTimeoutSeconds*time.Second {
		t.Fatalf(
			"RabbitMQPublishTimeout = %s, want %s",
			cfg.RabbitMQPublishTimeout,
			defaultPublishTimeoutSeconds*time.Second,
		)
	}
}

func TestLoadRabbitMQOverrides(t *testing.T) {
	t.Setenv("RABBITMQ_URL", "amqp://example")
	t.Setenv("RABBITMQ_EMOTION_QUEUE", "emotion.custom")
	t.Setenv("RABBITMQ_STT_QUEUE", "stt.custom")
	t.Setenv("RABBITMQ_REPLY_QUEUE", "reply.custom")
	t.Setenv("RABBITMQ_PUBLISH_TIMEOUT_SECONDS", "7")

	cfg := Load()

	if cfg.RabbitMQURL != "amqp://example" {
		t.Fatalf("RabbitMQURL = %q, want %q", cfg.RabbitMQURL, "amqp://example")
	}
	if cfg.RabbitMQEmotionQ != "emotion.custom" {
		t.Fatalf("RabbitMQEmotionQ = %q, want %q", cfg.RabbitMQEmotionQ, "emotion.custom")
	}
	if cfg.RabbitMQSTTQ != "stt.custom" {
		t.Fatalf("RabbitMQSTTQ = %q, want %q", cfg.RabbitMQSTTQ, "stt.custom")
	}
	if cfg.RabbitMQReplyQ != "reply.custom" {
		t.Fatalf("RabbitMQReplyQ = %q, want %q", cfg.RabbitMQReplyQ, "reply.custom")
	}
	if cfg.RabbitMQPublishTimeout != 7*time.Second {
		t.Fatalf("RabbitMQPublishTimeout = %s, want %s", cfg.RabbitMQPublishTimeout, 7*time.Second)
	}
}

func TestLoadInvalidPublishTimeoutUsesDefault(t *testing.T) {
	t.Setenv("RABBITMQ_PUBLISH_TIMEOUT_SECONDS", "invalid")

	cfg := Load()

	if cfg.RabbitMQPublishTimeout != defaultPublishTimeoutSeconds*time.Second {
		t.Fatalf(
			"RabbitMQPublishTimeout = %s, want %s",
			cfg.RabbitMQPublishTimeout,
			defaultPublishTimeoutSeconds*time.Second,
		)
	}
}

func TestLoadBackendSettings(t *testing.T) {
	t.Setenv("BACKEND_BASE_URL", "http://backend.example/")
	t.Setenv("BACKEND_REQUEST_TIMEOUT_SECONDS", "5")

	cfg := Load()

	if cfg.BackendBaseURL != "http://backend.example/" {
		t.Fatalf("BackendBaseURL = %q", cfg.BackendBaseURL)
	}
	if cfg.BackendRequestTimeout != 5*time.Second {
		t.Fatalf("BackendRequestTimeout = %s, want %s", cfg.BackendRequestTimeout, 5*time.Second)
	}
}

func TestLoadInvalidBackendTimeoutUsesDefault(t *testing.T) {
	t.Setenv("BACKEND_REQUEST_TIMEOUT_SECONDS", "invalid")

	cfg := Load()

	if cfg.BackendRequestTimeout != defaultBackendTimeoutSeconds*time.Second {
		t.Fatalf(
			"BackendRequestTimeout = %s, want %s",
			cfg.BackendRequestTimeout,
			defaultBackendTimeoutSeconds*time.Second,
		)
	}
}
