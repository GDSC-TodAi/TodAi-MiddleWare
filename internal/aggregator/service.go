package aggregator

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Hyuk-II/todai-middleware/pkg/model"
)

const (
	DefaultTimeout = 10 * time.Second

	StatusCompleted      = "completed"
	StatusPartialFailed  = "partial_failed"
	StatusFailed         = "failed"
	StatusPartialTimeout = "partial_timeout"
	StatusTimeout        = "timeout"
)

type JobState struct {
	JobID         string
	SessionID     string
	ElderID       string
	CorrelationID string

	EmotionResult *model.WorkerResponse
	STTResult     *model.WorkerResponse

	CreatedAt time.Time

	timer     *time.Timer
	completed bool
}

type FinalResult struct {
	JobID         string
	SessionID     string
	ElderID       string
	CorrelationID string
	Status        string
	Message       string
	WorkerType    string
}

type FinalStatusHandler func(ctx context.Context, result FinalResult) error

type Service struct {
	mu                 sync.Mutex
	states             map[string]*JobState
	timeout            time.Duration
	finalStatusHandler FinalStatusHandler
	onFinalized        func(FinalResult)
}

func NewService(timeout time.Duration, handlers ...FinalStatusHandler) *Service {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	service := &Service{
		states:  make(map[string]*JobState),
		timeout: timeout,
	}
	if len(handlers) > 0 {
		service.finalStatusHandler = handlers[0]
	}
	return service
}

func (s *Service) HandleWorkerResponse(
	ctx context.Context,
	response model.WorkerResponse,
) error {
	if err := validateResponse(response); err != nil {
		return err
	}

	key := stateKey(response.JobID, response.CorrelationID)

	s.mu.Lock()
	state, ok := s.states[key]
	if !ok {
		state = &JobState{
			JobID:         response.JobID,
			SessionID:     response.SessionID,
			ElderID:       response.ElderID,
			CorrelationID: response.CorrelationID,
			CreatedAt:     time.Now(),
		}
		s.states[key] = state
		state.timer = time.AfterFunc(s.timeout, func() {
			s.handleTimeout(key)
		})
	}

	// Duplicate responses from the same worker are ignored. The first response
	// remains authoritative until an explicit retry/version policy is added.
	switch response.WorkerType {
	case model.WorkerTypeEmotion:
		if state.EmotionResult != nil {
			s.mu.Unlock()
			return nil
		}
		responseCopy := response
		state.EmotionResult = &responseCopy
	case model.WorkerTypeSTT:
		if state.STTResult != nil {
			s.mu.Unlock()
			return nil
		}
		responseCopy := response
		state.STTResult = &responseCopy
	}

	if state.EmotionResult == nil || state.STTResult == nil {
		s.mu.Unlock()
		return nil
	}

	state.completed = true
	if state.timer != nil {
		state.timer.Stop()
	}
	delete(s.states, key)
	result := newFinalResult(
		state,
		resultStatus(state),
		model.WorkerTypeEmotion+","+model.WorkerTypeSTT,
	)
	s.mu.Unlock()

	s.finalize(ctx, result)
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, state := range s.states {
		if state.timer != nil {
			state.timer.Stop()
		}
		delete(s.states, key)
	}
}

func (s *Service) handleTimeout(key string) {
	s.mu.Lock()
	state, ok := s.states[key]
	if !ok || state.completed {
		s.mu.Unlock()
		return
	}

	state.completed = true
	delete(s.states, key)
	result := newFinalResult(state, timeoutStatus(state), arrivedWorkerTypes(state))
	s.mu.Unlock()

	s.finalize(context.Background(), result)
}

func (s *Service) finalize(ctx context.Context, result FinalResult) {
	log.Printf(
		"aggregator finalized | job_id=%s session_id=%s elder_id=%s correlation_id=%s worker_type=%s final_status=%s",
		result.JobID,
		result.SessionID,
		result.ElderID,
		result.CorrelationID,
		result.WorkerType,
		result.Status,
	)
	if s.finalStatusHandler != nil {
		if err := s.finalStatusHandler(ctx, result); err != nil {
			log.Printf(
				"final job status update failed | job_id=%s correlation_id=%s status=%s error=%v",
				result.JobID,
				result.CorrelationID,
				result.Status,
				err,
			)
		}
	}

	if s.onFinalized != nil {
		s.onFinalized(result)
	}
}

func validateResponse(response model.WorkerResponse) error {
	switch response.WorkerType {
	case model.WorkerTypeEmotion, model.WorkerTypeSTT:
	default:
		return fmt.Errorf("unknown worker_type %q", response.WorkerType)
	}

	switch response.Status {
	case model.WorkerStatusSuccess, model.WorkerStatusFailed:
		return nil
	default:
		return fmt.Errorf("unknown worker status %q", response.Status)
	}
}

func resultStatus(state *JobState) string {
	emotionSuccess := state.EmotionResult.Status == model.WorkerStatusSuccess
	sttSuccess := state.STTResult.Status == model.WorkerStatusSuccess

	switch {
	case emotionSuccess && sttSuccess:
		return StatusCompleted
	case !emotionSuccess && !sttSuccess:
		return StatusFailed
	default:
		return StatusPartialFailed
	}
}

func timeoutStatus(state *JobState) string {
	if state.EmotionResult != nil || state.STTResult != nil {
		return StatusPartialTimeout
	}
	return StatusTimeout
}

func arrivedWorkerTypes(state *JobState) string {
	switch {
	case state.EmotionResult != nil && state.STTResult != nil:
		return model.WorkerTypeEmotion + "," + model.WorkerTypeSTT
	case state.EmotionResult != nil:
		return model.WorkerTypeEmotion
	case state.STTResult != nil:
		return model.WorkerTypeSTT
	default:
		return "none"
	}
}

func newFinalResult(state *JobState, status, workerType string) FinalResult {
	return FinalResult{
		JobID:         state.JobID,
		SessionID:     state.SessionID,
		ElderID:       state.ElderID,
		CorrelationID: state.CorrelationID,
		Status:        status,
		Message:       statusMessage(status),
		WorkerType:    workerType,
	}
}

func statusMessage(status string) string {
	switch status {
	case StatusCompleted:
		return "Both emotion and stt workers completed"
	case StatusPartialFailed:
		return "One worker failed while the other completed"
	case StatusFailed:
		return "Both emotion and stt workers failed"
	case StatusPartialTimeout:
		return "Timed out waiting for one worker response"
	case StatusTimeout:
		return "Timed out waiting for worker responses"
	default:
		return status
	}
}

func stateKey(jobID, correlationID string) string {
	return jobID + "\x00" + correlationID
}
