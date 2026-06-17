# Spring Backend Internal API Contract

## 목적

Go 미들웨어는 RabbitMQ 작업의 생성, publish 이벤트, Aggregator 최종 상태를 기록하기 위해 Spring Backend 내부 API를 호출한다.

DB 접근과 트랜잭션은 Spring Backend가 담당한다. Go 미들웨어는 DB driver나 repository를 가지지 않는다.

전체 흐름:

```text
발화 종료
-> analysis_job 생성
-> RabbitMQ emotion/STT publish
-> publish 이벤트 기록
-> WorkerResponse 수신 및 Aggregator 결합
-> analysis_job 최종 상태 업데이트
```

## 환경변수

```env
BACKEND_BASE_URL=http://localhost:8080
BACKEND_REQUEST_TIMEOUT_SECONDS=3
```

- `BACKEND_BASE_URL`이 비어 있으면 backend 연동은 비활성화된다.
- 요청 timeout 기본값은 3초다.
- 시작 시 Backend 연결 확인 요청은 보내지 않는다.

## 공통 규칙

- Request/Response 형식: JSON
- Request `Content-Type`: `application/json`
- 성공 응답: 모든 2xx 상태
- `job_id` path parameter는 URL escape해서 전달
- 내부 API 인증 방식은 아직 정의되지 않았다.

## 1. Analysis Job 생성

```http
POST /internal/analysis-jobs
Content-Type: application/json
```

Request:

```json
{
  "session_id": "session-uuid",
  "elder_id": "1",
  "correlation_id": "correlation-uuid",
  "requested_workers": ["emotion", "stt"]
}
```

Response:

```json
{
  "job_id": "job-uuid",
  "status": "queued"
}
```

필드:

| 필드 | 필수 | 설명 |
| --- | --- | --- |
| `session_id` | 예 | WebSocket conversation session 식별자 |
| `elder_id` | 현재 빈 값 허용 | 향후 WebSocket identity에서 전달 |
| `correlation_id` | 예 | 한 발화의 Worker 요청/응답 묶음 식별자 |
| `requested_workers` | 예 | 현재 `emotion`, `stt` |
| `job_id` | 응답 필수 | Spring Backend가 생성한 작업 식별자 |
| `status` | 응답 필수 권장 | 최초 상태 `queued` |

Go 미들웨어는 응답 `job_id`가 비어 있으면 생성 실패로 처리한다.

## 2. Job Event 기록

```http
POST /internal/analysis-jobs/{job_id}/events
Content-Type: application/json
```

Request:

```json
{
  "event_type": "PUBLISHED",
  "worker_type": "emotion",
  "correlation_id": "correlation-uuid",
  "message": "emotion worker request published"
}
```

이벤트 타입:

| 값 | 의미 |
| --- | --- |
| `PUBLISH_STARTED` | 두 Worker publish 시작 |
| `PUBLISHED` | 특정 Worker 큐 publish 성공 |
| `PUBLISH_FAILED` | 특정 Worker 큐 publish 실패 |

`PUBLISH_STARTED`는 `worker_type`을 생략한다. `PUBLISHED`, `PUBLISH_FAILED`는 `emotion` 또는 `stt`를 전달한다.

## 3. Job 최종 상태 업데이트

```http
PATCH /internal/analysis-jobs/{job_id}/status
Content-Type: application/json
```

Request:

```json
{
  "status": "completed",
  "correlation_id": "correlation-uuid",
  "message": "Both emotion and stt workers completed"
}
```

상태 값:

| 값 | 의미 |
| --- | --- |
| `completed` | Emotion/STT 모두 성공 |
| `partial_failed` | 한 Worker 성공, 한 Worker 실패 |
| `failed` | Emotion/STT 모두 실패 |
| `partial_timeout` | 첫 응답 후 10초 안에 다른 응답 미도착 |
| `timeout` | 결과 없이 timeout 처리된 상태 |

## 장애 정책

### Job 생성 실패

- RabbitMQ publish를 시작하지 않는다.
- Go 미들웨어 로그에 오류를 기록한다.
- 추적할 DB job이 없는 상태에서 큐 작업만 생성되는 것을 방지한다.

### Event 기록 실패

- RabbitMQ publish 결과에는 영향을 주지 않는다.
- Go 미들웨어 로그만 남긴다.
- 현재 retry/DLQ는 없다.

### 최종 상태 업데이트 실패

- Aggregator 완료 결과와 Reply Queue Ack에는 영향을 주지 않는다.
- Go 미들웨어 로그만 남긴다.
- Aggregator 메모리 상태는 기존 정책대로 삭제한다.

### Backend 비활성화

- Go 미들웨어가 임시 UUID `job_id`를 생성한다.
- 기존 RabbitMQ publish/consume/Aggregator 흐름은 계속 동작한다.
- Event 및 최종 상태 API는 호출하지 않는다.

## 아직 구현하지 않은 것

- Spring Backend API 구현
- 내부 API 인증/인가
- `analysis_result` 저장
- `analysis_metric` 저장
- Google ADK 결과 저장
- Backend API retry 및 circuit breaker
- idempotency key와 중복 이벤트 처리 계약
- late WorkerResponse 상태 변경 정책
