# Backend Result Save 2B Implementation Plan

## 1. 목적

이번 2차-B 작업의 목표는 Aggregator final result와 ADK 처리 결과를 Spring Backend의 result 저장 API로 전송하는 것이다.

Spring Backend API는 다음 경로가 이미 구현되어 있다고 가정한다.

```http
POST /api/internal/analysis-jobs/{job_id}/result
```

현재 Go 미들웨어는 Aggregator final status 계산 후 Spring status update를 호출하고, emotion 결과와 STT text가 있을 때 ADK `/analyze`를 호출한다. 그러나 ADK 결과는 아직 로그로만 출력된다. 2차-B 구현은 이 빈칸을 채워 Spring에 `analysis_result`, `analysis_metric` 저장 요청을 보내는 작업이다.

## 2. 작업 범위

### 포함

- `internal/backend` 패키지에 result 저장 DTO 추가
- backend client에 `SaveAnalysisResult()` 추가
- result 저장 API 경로 추가
- Aggregator final result 이후 result 저장 요청 연결
- ADK `SUCCESS`, `FAILED`, `SKIPPED` 상태 구분
- STT text와 ADK metrics 전송
- ADK 성공 시 metrics 5개 전송
- ADK 실패/skipped 시 metrics `null` 전송
- backend client 단위 테스트 추가

### 제외

- Spring Backend 코드 수정
- RabbitMQ `WorkerRequest`, `WorkerResponse` 계약 변경
- emotion/STT Worker 로직 변경
- ADK `/analyze` API 계약 변경
- WebSocket 프로토콜 변경
- Fast Track 로직 변경
- emotion raw score를 Spring에 저장하는 기능 추가

## 3. 현재 흐름과 변경 후 흐름

현재 흐름:

```text
WebSocket audio chunk
-> VAD 발화 종료
-> Slow Track 실행
-> Spring에 analysis_job 생성
-> RabbitMQ emotion/STT worker 요청 publish
-> Reply Queue에서 WorkerResponse 수신
-> Aggregator가 emotion/STT 결과 결합
-> 최종 job status 계산
-> Spring에 job status update
-> emotion + stt_text가 있으면 ADK /analyze 호출
-> ADK 결과 로그 출력
```

변경 후 흐름:

```text
WebSocket audio chunk
-> VAD 발화 종료
-> Slow Track 실행
-> Spring에 analysis_job 생성
-> RabbitMQ emotion/STT worker 요청 publish
-> Reply Queue에서 WorkerResponse 수신
-> Aggregator가 emotion/STT 결과 결합
-> 최종 job status 계산
-> Spring에 job status update
-> ADK 호출 성공/실패/skipped 판단
-> Spring에 result 저장 요청
```

연결 위치는 `cmd/server/main.go`의 `buildFinalStatusHandler()`가 가장 자연스럽다. 이 함수는 이미 Aggregator final result를 받고, Spring status update와 ADK 호출을 수행하는 지점이다.

## 4. Spring Result 저장 API 계약

Endpoint:

```http
POST /api/internal/analysis-jobs/{job_id}/result
```

`BACKEND_BASE_URL`은 host 기준으로 유지한다.

```text
BACKEND_BASE_URL=http://localhost:8080
```

최종 호출 URL:

```text
http://localhost:8080/api/internal/analysis-jobs/{job_id}/result
```

### ADK 성공 요청

```json
{
  "session_id": "go-websocket-session-uuid",
  "elder_id": "",
  "correlation_id": "utterance-correlation-uuid",
  "job_status": "completed",
  "analysis_status": "SUCCESS",
  "adk_status": "SUCCESS",
  "stt_text": "오늘은 몸이 조금 무겁지만 괜찮아요.",
  "metrics": {
    "social_isolation": 0.2,
    "health_anxiety": 0.4,
    "daily_vitality": 0.7,
    "emotion_variance": 0.3,
    "cognitive_load": 0.5
  },
  "summary_text": null,
  "overall_score": null,
  "error_reason": null,
  "adk_error_reason": null
}
```

### ADK 실패 요청

```json
{
  "session_id": "go-websocket-session-uuid",
  "elder_id": "",
  "correlation_id": "utterance-correlation-uuid",
  "job_status": "completed",
  "analysis_status": "PARTIAL",
  "adk_status": "FAILED",
  "stt_text": "오늘은 몸이 조금 무겁지만 괜찮아요.",
  "metrics": null,
  "summary_text": null,
  "overall_score": null,
  "error_reason": null,
  "adk_error_reason": "ADK request failed"
}
```

### ADK skipped 요청

```json
{
  "session_id": "go-websocket-session-uuid",
  "elder_id": "",
  "correlation_id": "utterance-correlation-uuid",
  "job_status": "partial_timeout",
  "analysis_status": "PARTIAL",
  "adk_status": "SKIPPED",
  "stt_text": "오늘은 몸이 조금 무겁지만 괜찮아요.",
  "metrics": null,
  "summary_text": null,
  "overall_score": null,
  "error_reason": "one or more workers timed out",
  "adk_error_reason": null
}
```

중요한 제한:

- request body에 `emotion` 필드를 넣지 않는다.
- `emotion_sadness`, `emotion_anxiety`, `emotion_neutral`, `emotion_joy`를 보내지 않는다.
- Spring에는 ADK 최종 metrics만 보낸다.
- STT text는 분석 근거용으로 보낸다.

## 5. 추가할 DTO

`internal/backend/dto.go`에 추가할 DTO 후보:

```go
type SaveAnalysisResultRequest struct {
    SessionID      string         `json:"session_id"`
    ElderID        string         `json:"elder_id"`
    CorrelationID  string         `json:"correlation_id"`
    JobStatus      string         `json:"job_status"`
    AnalysisStatus string         `json:"analysis_status"`
    AdkStatus      string         `json:"adk_status"`
    STTText        *string        `json:"stt_text"`
    Metrics        *MetricPayload `json:"metrics"`
    SummaryText    *string        `json:"summary_text"`
    OverallScore   *float64       `json:"overall_score"`
    ErrorReason    *string        `json:"error_reason"`
    AdkErrorReason *string        `json:"adk_error_reason"`
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
    AdkStatus        string `json:"adk_status"`
    SavedMetricCount int    `json:"saved_metric_count"`
    UpdatedAt        string `json:"updated_at"`
}
```

pointer 필드는 JSON `null` 전송을 명확히 표현하기 위한 것이다.

## 6. 추가할 Backend Client 메서드

`internal/backend/client.go`에 추가할 메서드:

```go
func (c *Client) SaveAnalysisResult(
    ctx context.Context,
    jobID string,
    req SaveAnalysisResultRequest,
) (*SaveAnalysisResultResponse, error)
```

호출 경로:

```text
/api/internal/analysis-jobs/{job_id}/result
```

기존 `analysisJobsPath = "/api/internal/analysis-jobs"` 상수를 재사용하면 된다.

```go
path := analysisJobsPath + "/" + url.PathEscape(jobID) + "/result"
```

## 7. ADK 상태 결정 규칙

### SUCCESS

조건:

- emotion result 존재
- STT text 존재
- `ADK_BASE_URL` 설정됨
- ADK `/analyze` 호출 성공

저장값:

```text
analysis_status = SUCCESS
adk_status = SUCCESS
metrics = ADK response 5 metrics
adk_error_reason = null
```

### FAILED

조건:

- emotion result 존재
- STT text 존재
- `ADK_BASE_URL` 설정됨
- ADK `/analyze` 호출 실패

저장값:

```text
analysis_status = PARTIAL
adk_status = FAILED
metrics = null
adk_error_reason = ADK failure message
```

### SKIPPED

조건:

- emotion result 없음
- 또는 STT text 없음
- 또는 `ADK_BASE_URL` 미설정
- 또는 job final status가 ADK 호출 조건을 만족하지 않음

저장값:

```text
analysis_status = PARTIAL 또는 FAILED
adk_status = SKIPPED
metrics = null
adk_error_reason = null
```

## 8. analysis_status 결정 기준

| job_status | adk_status | analysis_status |
| --- | --- | --- |
| `completed` | `SUCCESS` | `SUCCESS` |
| `completed` | `FAILED` | `PARTIAL` |
| `completed` | `SKIPPED` | `PARTIAL` |
| `partial_failed` | `SKIPPED` | `PARTIAL` |
| `partial_timeout` | `SKIPPED` | `PARTIAL` |
| `failed` | `SKIPPED` | `FAILED` |
| `timeout` | `SKIPPED` | `FAILED` |

## 9. stt_text, metrics, error_reason 처리

### stt_text

- STT 결과가 있으면 문자열 pointer로 보낸다.
- STT 결과가 없으면 `null`로 보낸다.

### metrics

- ADK 성공 시에만 metrics를 보낸다.
- ADK 실패 또는 skipped이면 `null`로 보낸다.

### error_reason

`completed`이면 `null`로 보낸다.

그 외 상태는 명확한 Worker error가 없더라도 최소 메시지를 채울 수 있다.

| job_status | error_reason 예시 |
| --- | --- |
| `partial_failed` | `one or more workers failed` |
| `partial_timeout` | `one or more workers timed out` |
| `failed` | `all workers failed` |
| `timeout` | `all workers timed out` |

## 10. 호출 실패 처리

`SaveAnalysisResult()` 호출 실패 시 정책:

- 에러 로그를 남긴다.
- WebSocket, RabbitMQ, Aggregator 흐름을 중단하지 않는다.
- panic을 발생시키지 않는다.
- 현재 event enum에 없는 값을 무리하게 추가하지 않는다.
- 추후 Spring event 계약이 확장되면 `RESULT_SAVE_FAILED` 또는 `ADK_FAILED` 성격의 이벤트를 남길 수 있다.

## 11. 테스트 계획

backend client 단위 테스트:

| 번호 | 테스트 | 기대 |
| --- | --- | --- |
| 1 | `SaveAnalysisResult()` URL | `/api/internal/analysis-jobs/{job_id}/result`로 `POST` |
| 2 | ADK 성공 body | metrics 5개 포함 |
| 3 | ADK 성공 body | emotion raw score 필드 없음 |
| 4 | ADK 실패 body | metrics가 `null` |
| 5 | ADK skipped body | metrics가 `null` |
| 6 | STT text 없음 | `stt_text`가 `null` |
| 7 | `BACKEND_BASE_URL` 끝 `/` | URL 정상 생성 |
| 8 | Spring 응답 decode | `SaveAnalysisResultResponse`로 파싱 |

handler 연결 테스트 후보:

- ADK 성공 시 `SaveAnalysisResult()`가 `SUCCESS/SUCCESS`로 호출되는지 확인
- ADK 실패 시 `PARTIAL/FAILED`로 호출되는지 확인
- ADK skipped 시 `PARTIAL/SKIPPED` 또는 `FAILED/SKIPPED`로 호출되는지 확인
- result 저장 실패가 handler error로 전파되지 않는지 확인

최종 확인:

```bash
go test ./...
```

## 12. 구현 시 주의사항

- DTO 계약 외 기존 `CreateAnalysisJob`, `CreateJobEvent`, `UpdateJobStatus` 요청 구조는 변경하지 않는다.
- `WorkerRequest`, `WorkerResponse` 구조를 변경하지 않는다.
- Aggregator status 계산 로직을 변경하지 않는다.
- RabbitMQ publish/consume 로직을 변경하지 않는다.
- ADK `/analyze` request/response 계약을 변경하지 않는다.
- emotion raw score는 ADK 호출 입력으로만 사용하고 Spring result 저장 요청에는 포함하지 않는다.

## 13. 남은 작업

- 실제 Spring + Middleware 통합 실행 테스트
- RabbitMQ Worker 응답 후 result 저장까지 end-to-end 확인
- ADK 실패/timeout 케이스 확인
- 사회복지사 뷰에서 저장된 ADK metric 조회 연결 여부 검토
