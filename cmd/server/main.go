package main

import (
	"context"
	"log"

	"github.com/Hyuk-II/todai-middleware/internal/adk"
	"github.com/Hyuk-II/todai-middleware/internal/aggregator"
	"github.com/Hyuk-II/todai-middleware/internal/backend"
	"github.com/Hyuk-II/todai-middleware/internal/config"
	"github.com/Hyuk-II/todai-middleware/internal/fasttrack"
	"github.com/Hyuk-II/todai-middleware/internal/llm"
	"github.com/Hyuk-II/todai-middleware/internal/orchestrator"
	"github.com/Hyuk-II/todai-middleware/internal/queue"
	"github.com/Hyuk-II/todai-middleware/internal/slowtrack"
	"github.com/Hyuk-II/todai-middleware/internal/stt"
	"github.com/Hyuk-II/todai-middleware/internal/tts"
	"github.com/Hyuk-II/todai-middleware/internal/websocket"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file, using environment variables")
	}

	cfg := config.Load()

	// ── Spring Backend client ──────────────────────────────────────────────
	backendClient := backend.NewClient(cfg.BackendBaseURL, cfg.BackendRequestTimeout)
	if backendClient.Enabled() {
		log.Printf("backend integration enabled | base_url=%s", cfg.BackendBaseURL)
	} else {
		log.Printf("backend integration disabled")
	}

	// ── ADK client ────────────────────────────────────────────────────────
	adkClient := adk.NewClient(cfg.ADKBaseURL, cfg.ADKTimeout)
	if adkClient.Enabled() {
		log.Printf("adk enabled | base_url=%s", cfg.ADKBaseURL)
	} else {
		log.Printf("adk disabled (ADK_BASE_URL not set)")
	}

	// ── Fast Track clients ────────────────────────────────────────────────
	sttClient := stt.NewClient(cfg.STTBaseURL, cfg.STTTimeout)
	llmClient := llm.NewClient(cfg.LLMBaseURL, cfg.LLMModel, cfg.LLMTimeout)
	ttsClient := tts.NewClient(cfg.TTSBaseURL, cfg.TTSTimeout)
	log.Printf("fast track | stt=%v llm=%v tts=%v",
		sttClient.Enabled(), llmClient.Enabled(), ttsClient.Enabled())

	// ── WebSocket handler (audio handler set after orchestrator is built) ──
	wsHandler := websocket.NewHandler(nil)

	// ── Fast Track service ────────────────────────────────────────────────
	fastTrackSvc := fasttrack.NewService(sttClient, llmClient, ttsClient, wsHandler, cfg.FastTrackTimeout)

	// ── RabbitMQ + Slow Track ─────────────────────────────────────────────
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

		// Aggregator finalizes when both workers reply (or timeout).
		// After finalization: update Spring status + call ADK for 5 metrics.
		finalStatusHandler := buildFinalStatusHandler(backendClient, adkClient)
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

	// ── Orchestrator: VAD + fan-out to both tracks ─────────────────────────
	audioHandler := orchestrator.NewService(utterancePublisher, fastTrackSvc)
	wsHandler.SetAudioHandler(audioHandler)

	// ── HTTP routes ───────────────────────────────────────────────────────
	r := gin.Default()
	r.GET("/ws", wsHandler.ServeHTTP)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	log.Printf("todai-middleware starting on :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Printf("server error: %v", err)
	}
}

// buildFinalStatusHandler returns a handler that updates Spring status, evaluates
// ADK success/failure/skipped state, and saves the final analysis result.
func buildFinalStatusHandler(
	backendClient *backend.Client,
	adkClient *adk.Client,
) aggregator.FinalStatusHandler {
	return func(ctx context.Context, result aggregator.FinalResult) error {
		// 1. Update Spring Backend job status.
		if backendClient.Enabled() {
			if err := backendClient.UpdateJobStatus(
				ctx,
				result.JobID,
				backend.UpdateJobStatusRequest{
					Status:        result.Status,
					CorrelationID: result.CorrelationID,
					Message:       result.Message,
				},
			); err != nil {
				log.Printf("[%s] backend status update failed: %v", result.CorrelationID, err)
			}
		}

		saveReq := newSaveAnalysisResultRequest(result)

		// 2. Call ADK when both worker results are available.
		if adkClient.Enabled() && result.EmotionResult != nil && result.STTText != "" {
			adkCtx, cancel := context.WithTimeout(ctx, adkClient.Timeout())
			defer cancel()

			metrics, err := adkClient.Analyze(adkCtx, *result.EmotionResult, result.STTText)
			if err != nil {
				log.Printf("[%s] adk analysis failed: %v", result.CorrelationID, err)
				saveReq.ADKStatus = backend.ADKStatusFailed
				saveReq.ADKErrorReason = stringPtr(err.Error())
				saveReq.AnalysisStatus = analysisStatus(result.Status, saveReq.ADKStatus)
			} else {
				saveReq.ADKStatus = backend.ADKStatusSuccess
				saveReq.AnalysisStatus = analysisStatus(result.Status, saveReq.ADKStatus)
				saveReq.Metrics = metricPayload(metrics)
				log.Printf(
					"[%s] adk metrics | social_isolation=%.2f health_anxiety=%.2f daily_vitality=%.2f emotion_variance=%.2f cognitive_load=%.2f",
					result.CorrelationID,
					metrics.SocialIsolation,
					metrics.HealthAnxiety,
					metrics.DailyVitality,
					metrics.EmotionVariance,
					metrics.CognitiveLoad,
				)
			}
		}

		// 3. Persist final result when Backend integration is enabled.
		if backendClient.Enabled() {
			if _, err := backendClient.SaveAnalysisResult(ctx, result.JobID, saveReq); err != nil {
				log.Printf("[%s] backend result save failed: %v", result.CorrelationID, err)
			}
		}

		return nil
	}
}

func newSaveAnalysisResultRequest(result aggregator.FinalResult) backend.SaveAnalysisResultRequest {
	req := backend.SaveAnalysisResultRequest{
		SessionID:      result.SessionID,
		ElderID:        result.ElderID,
		CorrelationID:  result.CorrelationID,
		JobStatus:      result.Status,
		AnalysisStatus: analysisStatus(result.Status, backend.ADKStatusSkipped),
		ADKStatus:      backend.ADKStatusSkipped,
		ErrorReason:    errorReason(result.Status),
	}
	if result.STTText != "" {
		req.STTText = stringPtr(result.STTText)
	}
	return req
}

func metricPayload(metrics adk.MetricsResult) *backend.MetricPayload {
	return &backend.MetricPayload{
		SocialIsolation: metrics.SocialIsolation,
		HealthAnxiety:   metrics.HealthAnxiety,
		DailyVitality:   metrics.DailyVitality,
		EmotionVariance: metrics.EmotionVariance,
		CognitiveLoad:   metrics.CognitiveLoad,
	}
}

func analysisStatus(jobStatus string, adkStatus string) string {
	if jobStatus == aggregator.StatusCompleted && adkStatus == backend.ADKStatusSuccess {
		return backend.AnalysisStatusSuccess
	}
	switch jobStatus {
	case aggregator.StatusFailed, aggregator.StatusTimeout:
		return backend.AnalysisStatusFailed
	default:
		return backend.AnalysisStatusPartial
	}
}

func errorReason(jobStatus string) *string {
	var reason string
	switch jobStatus {
	case aggregator.StatusPartialFailed:
		reason = "one or more workers failed"
	case aggregator.StatusPartialTimeout:
		reason = "one or more workers timed out"
	case aggregator.StatusFailed:
		reason = "all workers failed"
	case aggregator.StatusTimeout:
		reason = "all workers timed out"
	default:
		return nil
	}
	return &reason
}

func stringPtr(value string) *string {
	return &value
}
