package main

import (
	"log"

	"github.com/Hyuk-II/todai-middleware/internal/config"
	"github.com/Hyuk-II/todai-middleware/internal/queue"
	"github.com/Hyuk-II/todai-middleware/internal/slowtrack"
	"github.com/Hyuk-II/todai-middleware/internal/websocket"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file, using environment variables")
	}

	cfg := config.Load()

	topology := queue.NewTopology(cfg.RabbitMQEmotionQ, cfg.RabbitMQSTTQ)
	queueClient, err := queue.NewClient(cfg.RabbitMQURL, topology)
	var audioHandler websocket.AudioChunkHandler
	if err != nil {
		log.Printf("rabbitmq unavailable, slow track publish disabled: %v", err)
	} else {
		defer func() {
			if err := queueClient.Close(); err != nil {
				log.Printf("rabbitmq close failed: %v", err)
			}
		}()
		publisher := queue.NewPublisher(queueClient)
		audioHandler = slowtrack.NewService(publisher)
		log.Printf("rabbitmq connected | emotion_queue=%s stt_queue=%s", cfg.RabbitMQEmotionQ, cfg.RabbitMQSTTQ)
	}

	r := gin.Default()

	wsHandler := websocket.NewHandler(audioHandler)
	r.GET("/ws", wsHandler.ServeHTTP)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	log.Printf("todai-middleware starting on :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
