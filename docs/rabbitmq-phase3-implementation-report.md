# RabbitMQ 3차 구현 결과 리포트

## 1. 변경 요약

Go 미들웨어가 DB에 직접 접근하지 않고 Spring Backend 내부 API를 통해 분석 작업 상태를 기록하도록 연동 구조를 추가했다.

- Backend HTTP client와 DTO 추가
- Backend base URL 및 timeout 설정 추가
- 발화 publish 전 `analysis_job` 생성
- Emotion/STT publish 결과 이벤트 기록
- Aggregator 최종 상태 Backend 업데이트
- Backend disabled fallback 유지
- Backend 장애 단계별 처리 정책 적용

## 2. 변경된 파일

| 파일 경로 | 변경 내용 |
| --- | --- |
| `.env.example` | Backend base URL과 request timeout 추가 |
| `internal/config/config.go` | Backend 설정 및 공통 초 단위 timeout parser 추가 |
| `internal/config/config_test.go` | Backend timeout/환경변수 테스트 추가 |
| `internal/backend/dto.go` | Job 생성, 이벤트, 상태 업데이트 DTO와 이벤트 상수 추가 |
| `internal/backend/client.go` | Spring Backend 내부 API HTTP client 구현 |
| `internal/backend/client_test.go` | HTTP 계약, path escape, 오류, context 테스트 추가 |
| `internal/slowtrack/service.go` | Job 생성, publish 이벤트 기록, 오류 반환 흐름 추가 |
| `internal/slowtrack/service_test.go` | Backend enabled/disabled 및 장애 정책 테스트 추가 |
| `internal/queue/publisher.go` | Worker별 publish 오류를 개별 반환하도록 변경 |
| `internal/queue/publisher_test.go` | Worker별 오류 반환 계약으로 테스트 갱신 |
| `internal/orchestrator/service.go` | Slow Track 오류 로그 처리 추가 |
| `internal/aggregator/service.go` | FinalStatusHandler와 평탄화된 FinalResult 추가 |
| `internal/aggregator/service_test.go` | 최종 상태 handler 실패 비영향 테스트 추가 |
| `cmd/server/main.go` | Backend client 생성 및 Slow Track/Aggregator 주입 |
| `docs/backend-internal-api-contract.md` | Spring Backend 내부 API 계약 문서 |

## 3. Backend Client 구조

`internal/backend.Client`는 다음 정보를 가진다.

```go
type Client struct {
    baseURL    string
    httpClient *http.Client
}
```

제공 메서드:

- `Enabled()`
- `CreateAnalysisJob()`
- `CreateJobEvent()`
- `UpdateJobStatus()`

공통 동작:

- JSON request body
- `Content-Type: application/json`
- context cancel/timeout 준수
- 2xx만 성공 처리
- 비-2xx 오류에 status code와 최대 4KB 응답 body 포함
- 응답 body close
- base URL trailing slash 제거
- `job_id` path escape

## 4. 추가된 환경변수

```env
BACKEND_BASE_URL=
BACKEND_REQUEST_TIMEOUT_SECONDS=3
```

- 빈 `BACKEND_BASE_URL`: backend 연동 비활성화
- timeout 기본값: 3초
- 잘못되거나 0 이하인 timeout: 3초 fallback

## 5. analysis_job 생성 흐름

Backend enabled 상태의 발화 처리:

```text
correlation_id 생성
-> POST /internal/analysis-jobs
-> Spring이 반환한 job_id 확인
-> WorkerRequest 두 개 생성
-> RabbitMQ publish
```

요청에는 다음 값이 포함된다.

- `session_id`
- 빈 문자열 상태의 `elder_id`
- `correlation_id`
- `requested_workers`: `emotion`, `stt`

Backend disabled 상태에서는 기존처럼 임시 UUID `job_id`를 생성한다.

Backend enabled 상태에서 job 생성이 실패하거나 빈 `job_id`가 반환되면 publish를 시작하지 않고 오류를 반환한다.

## 6. publish 이벤트 기록 흐름

Job 생성 이후 다음 이벤트를 기록한다.

1. Publish 전 `PUBLISH_STARTED`
2. Emotion 성공 시 `PUBLISHED`
3. Emotion 실패 시 `PUBLISH_FAILED`
4. STT 성공 시 `PUBLISHED`
5. STT 실패 시 `PUBLISH_FAILED`

Worker별 publish 오류를 구분하기 위해 `PublishToWorkers()`는 emotion/STT 오류를 개별 반환한다.

이벤트 API 실패는 로그만 남기며 실제 publish 성공/실패 결과를 변경하지 않는다.

## 7. Aggregator 최종 상태 업데이트 흐름

Aggregator에 다음 함수 타입을 추가했다.

```go
type FinalStatusHandler func(
    ctx context.Context,
    result FinalResult,
) error
```

`FinalResult`에는 다음 값이 포함된다.

- `job_id`
- `session_id`
- `elder_id`
- `correlation_id`
- 최종 `status`
- 설명 `message`
- 도착 Worker 종류

Backend enabled 상태에서 `main.go`가 handler를 구성해 다음 API를 호출한다.

```http
PATCH /internal/analysis-jobs/{job_id}/status
```

상태 업데이트 실패는 로그만 남기고 Aggregator 처리와 Reply Queue Ack를 실패시키지 않는다.

## 8. Spring Backend API 계약

다음 API 계약을 문서화했다.

- `POST /internal/analysis-jobs`
- `POST /internal/analysis-jobs/{job_id}/events`
- `PATCH /internal/analysis-jobs/{job_id}/status`

상세 Request/Response와 상태·이벤트 값은 `docs/backend-internal-api-contract.md`에 정리했다.

Spring Backend 코드는 이 저장소에 추가하지 않았다.

## 9. 장애 처리 정책

| 상황 | 처리 |
| --- | --- |
| Backend disabled | 임시 UUID job으로 기존 RabbitMQ 흐름 계속 |
| Job 생성 실패 | RabbitMQ publish 중단 및 오류 반환 |
| Publish event 기록 실패 | 로그만 남기고 publish 결과 유지 |
| 최종 상태 업데이트 실패 | 로그만 남기고 Aggregator 완료 처리 유지 |
| Backend base URL 설정, 서버 중단 | 첫 job 생성 시 HTTP 실패 후 publish 중단 |

서버 시작 시 Backend health check는 수행하지 않는다.

## 10. 아직 구현하지 않은 것

- Go DB 직접 연결
- Spring Backend API 구현
- DB migration 및 repository
- `analysis_result`, `analysis_metric` 저장
- Google ADK 호출
- Fast Track
- RabbitMQ retry/DLQ
- publisher confirm
- Python Worker 수정
- Backend API 인증, retry, circuit breaker

## 11. 테스트 결과

추가하거나 갱신한 테스트:

- Backend disabled 판정
- Job 생성 JSON 요청 및 2xx 응답
- base URL trailing slash 처리
- `job_id` path escape
- 비-2xx status/body 오류
- context cancellation
- Backend timeout 설정과 fallback
- Backend job ID가 WorkerRequest에 사용되는지 검증
- Job 생성 실패 시 publish 미호출
- Event API 실패가 publish를 실패시키지 않는지 검증
- Worker별 publish 실패 이벤트
- Aggregator final status handler 실패 비영향

실행 명령:

```bash
go test ./...
```

현재 작업 환경에는 `go`와 `gofmt` 실행 파일이 없어 테스트와 자동 포맷을 실행하지 못했다. `git diff --check`와 호출 관계, 금지 범위 검색은 수행했다.

## 12. 다음 구현 제안

1. Spring Backend에서 세 내부 API 구현
2. Go-Spring `httptest` 또는 staging 통합 테스트
3. WebSocket 인증 정보에서 `elder_id` 연결
4. 내부 API 인증 방식 정의
5. Job/event API idempotency key 정의
6. Backend API retry와 circuit breaker 정책 결정
7. Aggregator 최종 결과에서 `analysis_result` 저장 계약 설계
8. late WorkerResponse와 상태 재개방 정책 확정
