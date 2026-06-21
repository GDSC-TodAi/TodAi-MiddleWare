# Backend Result Save 2B Implementation Report

## 1. 요약

Go 미들웨어가 Aggregator final result 이후 Spring Backend result 저장 API를 호출하도록 2차-B 구현을 추가했다.

새 result 저장 API 경로:

```http
POST /api/internal/analysis-jobs/{job_id}/result
```

기존 흐름은 Spring job status update 후 ADK 결과를 로그로만 출력했다. 이제는 ADK 처리 상태를 `SUCCESS`, `FAILED`, `SKIPPED`로 구분하고, STT text와 ADK metrics를 Spring에 저장 요청으로 보낸다.

## 2. 수정/추가 파일

| 파일 | 내용 |
| --- | --- |
| `internal/backend/dto.go` | result 저장 DTO와 analysis/adk status 상수 추가 |
| `internal/backend/client.go` | `SaveAnalysisResult()` 추가 |
| `internal/backend/client_test.go` | result 저장 API client 테스트 추가 |
| `cmd/server/main.go` | Aggregator final handler에서 result 저장 연결 |
| `cmd/server/main_test.go` | ADK success/failed/skipped handler 연결 테스트 추가 |

## 3. 추가한 DTO

추가한 DTO:

- `SaveAnalysisResultRequest`
- `MetricPayload`
- `SaveAnalysisResultResponse`

추가한 status 상수:

- `AnalysisStatusSuccess = "SUCCESS"`
- `AnalysisStatusPartial = "PARTIAL"`
- `AnalysisStatusFailed = "FAILED"`
- `ADKStatusSuccess = "SUCCESS"`
- `ADKStatusFailed = "FAILED"`
- `ADKStatusSkipped = "SKIPPED"`

`SaveAnalysisResultRequest`는 `stt_text`, `metrics`, `summary_text`, `overall_score`, `error_reason`, `adk_error_reason`을 pointer 필드로 둔다. 값이 없으면 Spring에 JSON `null`로 전송된다.

## 4. 추가한 Backend Client 메서드

`internal/backend/client.go`에 다음 메서드를 추가했다.

```go
func (c *Client) SaveAnalysisResult(
    ctx context.Context,
    jobID string,
    req SaveAnalysisResultRequest,
) (*SaveAnalysisResultResponse, error)
```

호출 경로는 기존 `analysisJobsPath = "/api/internal/analysis-jobs"` 상수를 재사용한다.

```go
path := analysisJobsPath + "/" + url.PathEscape(jobID) + "/result"
```

따라서 `BACKEND_BASE_URL=http://localhost:8080`이면 최종 호출 URL은 다음과 같다.

```text
http://localhost:8080/api/internal/analysis-jobs/{job_id}/result
```

## 5. Result 저장 연결 위치

연결 위치는 `cmd/server/main.go`의 `buildFinalStatusHandler()`다.

처리 순서:

1. Aggregator final status를 Spring에 update
2. 기본 result 저장 요청 생성
3. ADK 호출 가능 여부 판단
4. ADK 성공/실패/skipped 상태 결정
5. Backend enabled 상태면 `SaveAnalysisResult()` 호출

status update 실패와 result 저장 실패는 모두 로그만 남기고 handler 전체 흐름을 중단하지 않는다.

## 6. ADK 성공/실패/skipped 처리

| 조건 | adk_status | analysis_status | metrics | adk_error_reason |
| --- | --- | --- | --- | --- |
| emotion result 있음, STT text 있음, ADK enabled, ADK 성공 | `SUCCESS` | `SUCCESS` | ADK 5개 metric | `null` |
| emotion result 있음, STT text 있음, ADK enabled, ADK 실패 | `FAILED` | `PARTIAL` | `null` | ADK error message |
| emotion result 없음 | `SKIPPED` | job status 기준 | `null` | `null` |
| STT text 없음 | `SKIPPED` | job status 기준 | `null` | `null` |
| ADK disabled | `SKIPPED` | job status 기준 | `null` | `null` |

`analysis_status` 결정 기준:

| job_status | adk_status | analysis_status |
| --- | --- | --- |
| `completed` | `SUCCESS` | `SUCCESS` |
| `completed` | `FAILED` | `PARTIAL` |
| `completed` | `SKIPPED` | `PARTIAL` |
| `partial_failed` | `SKIPPED` | `PARTIAL` |
| `partial_timeout` | `SKIPPED` | `PARTIAL` |
| `failed` | `SKIPPED` | `FAILED` |
| `timeout` | `SKIPPED` | `FAILED` |

## 7. 전송하지 않는 데이터

Spring result 저장 요청에는 emotion raw score를 포함하지 않는다.

보내지 않는 필드:

- `emotion`
- `emotion_sadness`
- `emotion_anxiety`
- `emotion_neutral`
- `emotion_joy`

emotion score는 ADK 호출 입력으로만 사용한다. Spring에는 ADK가 반환한 최종 5개 metric만 `metrics`로 보낸다.

## 8. 테스트 내용

추가/수정한 테스트:

- `SaveAnalysisResult()`가 `/api/internal/analysis-jobs/{job_id}/result`로 `POST`하는지 확인
- trailing slash가 있는 `BACKEND_BASE_URL`에서도 정상 경로 생성 확인
- ADK 성공 요청 body에 metrics 5개가 포함되는지 확인
- ADK 성공 요청 body에 emotion raw field가 없는지 확인
- metrics와 STT text가 nil이면 JSON `null`로 전송되는지 확인
- Spring 응답을 `SaveAnalysisResultResponse`로 decode하는지 확인
- final handler에서 ADK success result 저장 확인
- final handler에서 ADK failed result 저장 확인
- final handler에서 ADK skipped result 저장 확인

## 9. 테스트 실행 상태

현재 작업 환경에는 `go`와 `gofmt` 실행 파일이 없어 다음 명령을 실행하지 못했다.

```bash
gofmt -w ...
go test ./...
```

대신 정적 검증으로 `git diff --check`를 수행했고 통과했다.

## 10. 남은 작업

- 실제 Spring + Middleware 통합 실행 테스트
- RabbitMQ Worker 응답 후 result 저장까지 end-to-end 확인
- ADK 실패/timeout 케이스 확인
- 사회복지사 뷰에서 저장된 ADK metric 조회 연결 여부 검토
