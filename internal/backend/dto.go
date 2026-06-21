package backend

const (
	EventTypePublishStarted = "PUBLISH_STARTED"
	EventTypePublished      = "PUBLISHED"
	EventTypePublishFailed  = "PUBLISH_FAILED"
)

type CreateAnalysisJobRequest struct {
	SessionID        string   `json:"session_id"`
	ElderID          string   `json:"elder_id"`
	CorrelationID    string   `json:"correlation_id"`
	RequestedWorkers []string `json:"requested_workers"`
}

type CreateAnalysisJobResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

type CreateJobEventRequest struct {
	EventType     string `json:"event_type"`
	WorkerType    string `json:"worker_type,omitempty"`
	CorrelationID string `json:"correlation_id"`
	Message       string `json:"message,omitempty"`
}

type UpdateJobStatusRequest struct {
	Status        string `json:"status"`
	CorrelationID string `json:"correlation_id"`
	Message       string `json:"message,omitempty"`
}

const (
	AnalysisStatusSuccess = "SUCCESS"
	AnalysisStatusPartial = "PARTIAL"
	AnalysisStatusFailed  = "FAILED"

	ADKStatusSuccess = "SUCCESS"
	ADKStatusFailed  = "FAILED"
	ADKStatusSkipped = "SKIPPED"
)

type SaveAnalysisResultRequest struct {
	SessionID      string         `json:"session_id"`
	ElderID        string         `json:"elder_id"`
	CorrelationID  string         `json:"correlation_id"`
	JobStatus      string         `json:"job_status"`
	AnalysisStatus string         `json:"analysis_status"`
	ADKStatus      string         `json:"adk_status"`
	STTText        *string        `json:"stt_text"`
	Metrics        *MetricPayload `json:"metrics"`
	SummaryText    *string        `json:"summary_text"`
	OverallScore   *float64       `json:"overall_score"`
	ErrorReason    *string        `json:"error_reason"`
	ADKErrorReason *string        `json:"adk_error_reason"`
}

type MetricPayload struct {
	SocialIsolation float64 `json:"social_isolation"`
	HealthAnxiety   float64 `json:"health_anxiety"`
	DailyVitality   float64 `json:"daily_vitality"`
	EmotionVariance float64 `json:"emotion_variance"`
	CognitiveLoad   float64 `json:"cognitive_load"`
}

type SaveAnalysisResultResponse struct {
	ResultID         int64  `json:"result_id"`
	JobID            string `json:"job_id"`
	AnalysisStatus   string `json:"analysis_status"`
	ADKStatus        string `json:"adk_status"`
	SavedMetricCount int    `json:"saved_metric_count"`
	UpdatedAt        string `json:"updated_at"`
}
