package main

import (
	"context"
	"log"

	"github.com/Hyuk-II/todai-middleware/internal/aggregator"
	"github.com/Hyuk-II/todai-middleware/internal/backend"
	"github.com/Hyuk-II/todai-middleware/internal/config"
	"github.com/Hyuk-II/todai-middleware/internal/orchestrator"
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
	backendClient := backend.NewClient(cfg.BackendBaseURL, cfg.BackendRequestTimeout)
	if backendClient.Enabled() {
		log.Printf("backend integration enabled | base_url=%s", cfg.BackendBaseURL)
	} else {
		log.Printf("backend integration disabled")
	}

	topology := queue.NewTopology(cfg.RabbitMQEmotionQ, cfg.RabbitMQSTTQ, cfg.RabbitMQReplyQ)
	queueClient, err := queue.NewClient(cfg.RabbitMQURL, topology)

	var utterancePublisher orchestrator.UtterancePublisher
	if err != nil {
		log.Printf("rabbitmq unavailable, slow track publish disabled: %v", err)
	} else {
		defer func() {
			if err := queueClient.Close(); err != nil {
				log.Printf("rabbitmq close failed: %v", err)
			}
		}()

		var finalStatusHandler aggregator.FinalStatusHandler
		if backendClient.Enabled() {
			finalStatusHandler = func(ctx context.Context, result aggregator.FinalResult) error {
				return backendClient.UpdateJobStatus(
					ctx,
					result.JobID,
					backend.UpdateJobStatusRequest{
						Status:        result.Status,
						CorrelationID: result.CorrelationID,
						Message:       result.Message,
					},
				)
			}
		}
		agg := aggregator.NewService(aggregator.DefaultTimeout, finalStatusHandler)
		defer agg.Close()

		replyConsumer, consumerErr := queue.NewConsumer(queueClient, topology)
		if consumerErr != nil {
			log.Printf("reply consumer unavailable: %v", consumerErr)
		} else {
			defer func() {
				if err := replyConsumer.Close(); err != nil {
					log.Printf("reply consumer close failed: %v", err)
				}
			}()
			consumerCtx, cancelConsumer := context.WithCancel(context.Background())
			defer cancelConsumer()
			go func() {
				if err := replyConsumer.ConsumeReplies(consumerCtx, agg.HandleWorkerResponse); err != nil {
					log.Printf("reply consumer stopped: %v", err)
				}
			}()
		}

		utterancePublisher = slowtrack.NewService(
			queue.NewPublisher(queueClient),
			backendClient,
			cfg.RabbitMQReplyQ,
			cfg.RabbitMQPublishTimeout,
		)
		log.Printf(
			"rabbitmq connected | emotion_queue=%s stt_queue=%s reply_queue=%s",
			cfg.RabbitMQEmotionQ,
			cfg.RabbitMQSTTQ,
			cfg.RabbitMQReplyQ,
		)
	}

	// orchestrator는 RabbitMQ 없이도 항상 동작 (VAD + 버퍼링)
	// utterancePublisher가 nil이면 발화 감지만 하고 publish는 스킵
	audioHandler := orchestrator.NewService(utterancePublisher)

	r := gin.Default()

	wsHandler := websocket.NewHandler(audioHandler)
	r.GET("/ws", wsHandler.ServeHTTP)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	log.Printf("todai-middleware starting on :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		// Return from main normally so deferred RabbitMQ cleanup can run.
		log.Printf("server error: %v", err)
		return
	}
}
