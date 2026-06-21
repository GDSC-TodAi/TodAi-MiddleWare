package backend

const (
	EventTypePublishStarted = "PUBLISH_STARTED"
	// 백엔드 AnalysisJobEventType enum 의 정식 값은 PUBLISH_SUCCESS 다("PUBLISHED" 아님).
	EventTypePublished     = "PUBLISH_SUCCESS"
	EventTypePublishFailed = "PUBLISH_FAILED"
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
