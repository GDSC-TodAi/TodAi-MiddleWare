package config

import "os"

type Config struct {
	Port               string
	RabbitMQURL        string
	RabbitMQEmotionQ   string
	RabbitMQSTTQ       string
}

func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@localhost:5672/"
	}

	emotionQueue := os.Getenv("RABBITMQ_EMOTION_QUEUE")
	if emotionQueue == "" {
		emotionQueue = "todai.worker.emotion"
	}

	sttQueue := os.Getenv("RABBITMQ_STT_QUEUE")
	if sttQueue == "" {
		sttQueue = "todai.worker.stt"
	}

	return &Config{
		Port:             port,
		RabbitMQURL:      rabbitURL,
		RabbitMQEmotionQ: emotionQueue,
		RabbitMQSTTQ:     sttQueue,
	}
}
