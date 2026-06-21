# Middleware-Spring DB Contract Review

## 1. 요약 결론

현재 Spring DB 관계 초안의 큰 방향은 Go 미들웨어 구현과 대체로 맞다. 특히 `conversation_session 1:N analysis_job`, `analysis_job 1:N analysis_job_event`, `analysis_job 1:0..1 analysis_result`, `analysis_result 1:N analysis_metric` 구조는 현재 Slow Track이 "발화 단위 job"으로 동작하는 방식과 잘 맞는다.

반드시 수정해야 할 점은 API 필드 계약이다. 현재 Go 코드는 `conversation_session_id`가 아니라 WebSocket 연결 시 자체 생성한 문자열 `session_id`를 보낸다. `elder_id`도 현재 빈 문자열이다. 또한 `job_id`는 Go에서 `string`으로 받기 때문에 Spring이 Long PK를 JSON number로 반환하면 현재 Go DTO와 호환되지 않는다. Spring이 문자열로 반환하거나 Go DTO를 numeric으로 바꿔야 한다.

선택적으로 조정 가능한 점은 enum 명명이다. Go 내부 status와 worker type은 소문자 문자열이고, Spring enum 후보는 대문자다. Spring API boundary에서 명시적으로 변환할지, Go가 Spring enum 값을 직접 보내도록 바꿀지 결정해야 한다.

## 2. 실제 미들웨어 흐름

현재 실제 흐름은 다음과 같다.

```text
WebSocket 연결
-> session_id UUID 생성
-> binary audio chunk 수신
-> orchestrator가 session별 audio buffer + VAD 처리
-> VAD가 발화 종료 감지
-> Fast Track goroutine 실행
-> Slow Track goroutine 실행
-> Slow Track에서 correlation_id UUID 생성
-> Backend enabled면 Spring analysis_job 생성
-> Backend disabled면 Go 임시 UUID job_id 생성
-> 같은 job_id/correlation_id로 emotion/STT WorkerRequest 생성
-> RabbitMQ default exchange로 emotion/STT queue publish
-> Reply Queue에서 WorkerResponse consume
-> Aggregator가 job_id + correlation_id 기준으로 응답 결합
-> 두 Worker 응답 도착 또는 10초 timeout 시 final status 생성
-> Spring status update 호출
-> emotion 결과와 STT text가 있으면 ADK /analyze 호출
-> ADK 결과는 현재 로그만 출력
```

근거 파일/함수:

- `internal/websocket/handler.go`: `ServeHTTP`, `readLoop`
- `internal/orchestrator/service.go`: `HandleAudioChunk`
- `internal/slowtrack/service.go`: `PublishUtterance`, `createJob`
- `internal/queue/publisher.go`: `PublishToWorkers`, `publish`
- `internal/queue/consumer.go`: `ConsumeReplies`
- `internal/aggregator/service.go`: `HandleWorkerResponse`, `handleTimeout`, `newFinalResult`
- `cmd/server/main.go`: `buildFinalStatusHandler`
- `internal/adk/client.go`: `Analyze`

Slow Track 단위 검토:

| 항목 | 실제 구현 | DB 설계와 일치 여부 | 근거 파일/함수 |
| -- | ----- | ------------ | -------- |
| `analysis_job` 생성 단위 | VAD 발화 종료 후 `PublishUtterance()` 1회마다 생성 | 일치 | `orchestrator.HandleAudioChunk`, `slowtrack.PublishUtterance` |
| WebSocket session 단위 생성 여부 | 아님. WebSocket 연결은 session state이고, job은 발화마다 생성 | 일치 | `websocket.ServeHTTP`, `orchestrator.HandleAudioChunk` |
| audio chunk 단위 생성 여부 | 아님. chunk는 buffer에 누적되고 VAD 종료 시 발화로 처리 | 일치 | `orchestrator.HandleAudioChunk` |
| VAD 발화 종료 시 Slow Track 실행 | 맞음. VAD true 시 goroutine으로 `PublishUtterance()` 호출 | 일치 | `orchestrator.HandleAudioChunk` |
| 하나의 Slow Track 실행과 `correlation_id` | `PublishUtterance()` 시작 시 새 UUID 생성 | 일치 | `slowtrack.PublishUtterance` |

## 3. Spring Backend 호출 계약

현재 Go 미들웨어가 구현한 Spring Backend 호출은 세 가지뿐이다. result 저장 API는 없다.

| 호출 목적 | Method | Path | 요청 필드 | 응답 필드 | 현재 구현 여부 | 설계와 차이 |
| ----- | ------ | ---- | ----- | ----- | -------- | ------ |
| job 생성 | `POST` | `/internal/analysis-jobs` | `session_id`, `elder_id`, `correlation_id`, `requested_workers` | `job_id`, `status` | 구현됨 | 초안의 `conversation_session_id`, `job_type`, `request_payload` 없음. Go는 `job_id`를 string으로 기대 |
| job event 기록 | `POST` | `/internal/analysis-jobs/{job_id}/events` | `event_type`, `worker_type`, `correlation_id`, `message` | 없음 | 구현됨 | 초안의 `event_status`, `queue_name`, `routing_key`, `error_reason`, `payload_json`, `occurred_at` 없음 |
| job status 업데이트 | `PATCH` | `/internal/analysis-jobs/{job_id}/status` | `status`, `correlation_id`, `message` | 없음 | 구현됨 | 초안의 `error_reason`, `finished_at` 없음. Go status는 lowercase |
| result 저장 | `POST` | `/internal/analysis-jobs/{job_id}/result` | 없음 | 없음 | 미구현 | 초안 API 필요하지만 현재 Go client/DTO 없음 |

실패 시 fallback 정책:

- Backend disabled: `CreateAnalysisJob()`을 호출하지 않고 Go가 임시 UUID `job_id`를 생성한다.
- job 생성 실패: RabbitMQ publish를 시작하지 않고 `PublishUtterance()`가 error를 반환한다.
- event 기록 실패: 로그만 남기고 publish 흐름은 계속 진행한다.
- status update 실패: 로그만 남기고 Aggregator 완료 처리는 유지한다.
- result 저장 실패: 현재 result 저장 코드가 없으므로 정책도 없다.

## 4. WorkerRequest / WorkerResponse 계약

### WorkerRequest

실제 구조는 `pkg/model/message.go`의 `WorkerRequest`다.

| 필드 | 타입 | 설명 | DB/API 반영 필요 여부 |
| --- | --- | --- | --- |
| `job_id` | `string` | Spring 응답 `job_id` 또는 Backend disabled 시 Go 임시 UUID | 필요. Spring Long PK 사용 시 string/numeric 계약 정리 필요 |
| `session_id` | `string` | WebSocket 연결 시 Go가 만든 UUID | 필요. 현재는 DB PK가 아니라 session key에 가까움 |
| `elder_id` | `string` | 현재 빈 문자열 | 필요. Spring/API에서 채우는 경로 추가 필요 |
| `correlation_id` | `string` | 발화 단위 UUID | 필요. job 추적과 Worker 응답 매칭에 핵심 |
| `worker_type` | `string` | `emotion` 또는 `stt` | 필요. event 및 worker response 구분에 필요 |
| `reply_to` | `string` | Worker가 응답을 보낼 Reply Queue 이름 | event 또는 운영 로그에는 유용, result에는 불필요 |
| `audio_data` | `[]byte` | PCM 16bit, 16kHz, Mono로 기대되는 음성 payload | DB 저장 비추천. 요청 payload audit가 필요하면 별도 object storage 고려 |
| `timestamp` | `int64` | 요청 생성 시 Unix milliseconds | event occurred time으로 변환 가능 |

현재 `session_id`는 실제로 채워진다. 다만 WebSocket 연결 시 Go가 생성한 UUID이며 Spring `conversation_session.id`라는 보장은 없다. `elder_id`는 TODO와 함께 빈 문자열로 채워진다.

### WorkerResponse

실제 구조는 `pkg/model/message.go`의 `WorkerResponse`다.

| 항목 | 실제 구현 | Spring DB/API 영향 |
| --- | --- | --- |
| 필드명 | `job_id`, `session_id`, `elder_id`, `correlation_id`, `worker_type`, `status`, `result`, `error_message`, `timestamp` | event/result 저장 DTO가 이 필드를 수용해야 함 |
| status 값 | `success`, `failed`만 Aggregator가 허용 | Spring enum과 매핑 필요. `ok`, `error`, `partial`은 현재 Go 기준 불일치 |
| emotion result 구조 | `{"sadness": number, "anxiety": number, "neutral": number, "joy": number}` | `analysis_result` 또는 metric source payload에 저장 가능 |
| STT result 구조 | `{"text": string}` | `analysis_result.stt_text`에 저장 가능 |
| error 필드 | `error_message` string | `analysis_job_event.error_reason` 또는 `analysis_result.error_reason`에 매핑 가능 |

`WorkerReply`라는 deprecated 구조도 남아 있지만, 현재 Reply Queue consumer와 Aggregator는 `WorkerResponse`를 사용한다.

## 5. Aggregator status 계약

Aggregator는 `job_id + correlation_id`를 key로 메모리 state를 만든다. 첫 응답 도착 시 10초 timer를 시작하고, 두 Worker 응답이 모두 오거나 timeout이 발생하면 final result를 만든다.

| 상황 | Go 최종 status | Spring enum 후보 | 일치 여부 |
| -- | ------------ | -------------- | ----- |
| emotion/STT 둘 다 `success` | `completed` | `COMPLETED` | 의미 일치, 대소문자 변환 필요 |
| 한 Worker `success`, 한 Worker `failed` | `partial_failed` | `PARTIAL_FAILED` | 의미 일치, 대소문자 변환 필요 |
| 둘 다 `failed` | `failed` | `FAILED` | 의미 일치, 대소문자 변환 필요 |
| 일부만 도착 후 timeout | `partial_timeout` | `PARTIAL_TIMEOUT` | 의미 일치, 대소문자 변환 필요 |
| 아무 응답 없이 timeout | `timeout` | `TIMEOUT` | 의미 일치, 대소문자 변환 필요 |

Spring status update에는 Go status 문자열이 그대로 들어간다. 현재 코드에는 `completed -> COMPLETED` 같은 변환이 없다.

correlation_id 검토:

| 항목 | 실제 구현 |
| --- | --- |
| 생성 위치 | `slowtrack.PublishUtterance()` 시작 시 `uuid.New().String()` |
| 생성 단위 | VAD로 확정된 발화 단위 Slow Track 실행 1회 |
| WorkerRequest 포함 여부 | 포함됨. emotion/STT 요청 모두 같은 값 사용 |
| WorkerResponse 기대 여부 | 포함 필요. Aggregator key와 로그에 사용 |
| Aggregator key 포함 여부 | 포함됨. key는 `job_id + "\x00" + correlation_id` |
| DB unique 적절성 | `analysis_job.correlation_id UNIQUE`는 현재 발화 단위와 맞음. 다만 Backend disabled 임시 job까지 포함한 운영 경계는 별도 고려 |

## 6. ADK 계약

| 항목 | 실제 구현 | DB/API 영향 |
| --- | --- | --- |
| 호출 위치 | `cmd/server/main.go`의 `buildFinalStatusHandler()` | Aggregator finalization 이후가 자연스러운 위치 |
| 호출 조건 | `ADK_BASE_URL`이 있고 `EmotionResult != nil`이며 `STTText != ""` | 둘 다 성공적으로 파싱된 경우에만 호출됨 |
| partial/timeout 호출 | 결과 둘 중 하나가 없으면 호출하지 않음 | partial/timeout 저장 정책이 별도 필요 |
| request 구조 | `{"emotion": {...}, "text": "..."}` | result 저장 API에 같은 입력값 또는 원본 결과 저장 권장 |
| response 구조 | `social_isolation`, `health_anxiety`, `daily_vitality`, `emotion_variance`, `cognitive_load` | `analysis_metric` 5개 row로 매핑 가능 |
| 실패 처리 | 로그만 남김. status update는 이미 별도로 수행됨 | `analysis_result.adk_status=FAILED`, `adk_error_reason` 저장 필요 |
| Spring 저장 여부 | 없음. ADK 성공 결과도 로그만 출력 | `POST /internal/analysis-jobs/{job_id}/result` 필요 |

ADK 응답 필드와 Spring metric enum 후보 매핑은 적절하다.

| ADK field | Spring MetricType |
| --- | --- |
| `social_isolation` | `SOCIAL_ISOLATION` |
| `health_anxiety` | `HEALTH_ANXIETY` |
| `daily_vitality` | `DAILY_VITALITY` |
| `emotion_variance` | `EMOTION_VARIANCE` |
| `cognitive_load` | `COGNITIVE_LOAD` |

## 7. DB 설계 적합성 평가

| 설계 항목 | 평가 | 이유 | 수정 제안 |
| --- | --- | --- | --- |
| `conversation_session 1:N analysis_job` | 적합 | WebSocket session 하나에서 여러 발화가 발생하고 각 발화가 Slow Track job이 됨 | 현재 Go의 `session_id`가 DB PK인지 session key인지 계약 확정 필요 |
| `analysis_job 1:N analysis_job_event` | 적합 | publish started, worker별 publish result, worker reply, aggregation, ADK event를 모두 표현 가능 | 현재 Go event DTO가 너무 단순하므로 queue/routing/status/payload 필드 추가 |
| `analysis_job 1:0..1 analysis_result` | 적합 | 발화 단위 job 하나가 최종 분석 결과 하나를 만드는 구조 | result 저장 API는 upsert 또는 idempotent insert 권장 |
| `analysis_result 1:N analysis_metric` | 적합 | ADK가 5개 metric을 반환하므로 row 분리가 자연스러움 | metric type unique constraint 권장: `(analysis_result_id, metric_type)` |
| `analysis_job.correlation_id UNIQUE` | 적합 | Go가 발화 단위로 UUID를 생성하고 emotion/STT가 공유함 | `correlation_id`는 nullable 금지 권장 |
| `queue_name/routing_key를 event에 저장` | 적합 | 하나의 job이 emotion/STT 두 queue로 fan-out되므로 job row에 단일 queue를 둘 수 없음 | event row에 worker별 queue/routing key 저장 |
| `ADK status를 result에 분리 저장` | 적합 | Aggregator 성공과 ADK 성공은 별도 단계임 | `analysis_job.status`는 job/worker aggregation 상태, `analysis_result.adk_status`는 ADK 상태로 분리 |
| `job_id를 Spring Long PK로 사용` | 수정 필요 | Go DTO는 `job_id string`이다. Spring이 JSON number로 `1`을 반환하면 unmarshal 실패 가능 | Spring 응답을 `"1"`로 주거나 Go DTO를 `int64`/custom type으로 변경 |

## 8. 수정이 필요한 DB/API 설계

### 꼭 수정해야 하는 것

- `POST /internal/analysis-jobs` 요청 계약을 현재 Go와 맞춰야 한다.
  - 현재 Go 필드: `session_id`, `elder_id`, `correlation_id`, `requested_workers`
  - 초안 필드: `elder_id`, `conversation_session_id`, `correlation_id`, `job_type`, `request_payload`
  - 둘 중 하나로 통일해야 한다.
- `job_id` JSON 타입을 확정해야 한다.
  - 현재 Go는 string으로 decode한다.
  - Spring Long PK를 number로 반환하려면 Go 코드 수정이 필요하다.
- status enum 대소문자 변환 정책이 필요하다.
  - 현재 Go는 `completed`, `partial_failed`, `failed`, `partial_timeout`, `timeout`을 그대로 보낸다.
  - Spring enum은 `COMPLETED`, `PARTIAL_FAILED`, `FAILED`, `PARTIAL_TIMEOUT`, `TIMEOUT`이다.
- result 저장 API가 필요하다.
  - 현재 ADK 결과는 로그로만 남는다.
  - `analysis_result`, `analysis_metric`을 채우려면 API와 Go client method가 필요하다.

### 수정하면 좋은 것

- event API에 `event_status`, `queue_name`, `routing_key`, `error_reason`, `payload_json`, `occurred_at` 추가
- `WORKER_REPLY_RECEIVED`, `WORKER_REPLY_FAILED`, `AGGREGATION_COMPLETED`, `AGGREGATION_TIMEOUT`, `ADK_REQUESTED`, `ADK_SUCCESS`, `ADK_FAILED`, `RESULT_SAVED` event 추가
- result 저장 API를 idempotent하게 설계
  - 예: `PUT /internal/analysis-jobs/{job_id}/result`
  - 또는 `POST`를 유지하되 `job_id` unique 기반 upsert
- ADK 실패 시에도 result row를 저장하도록 설계
  - `adk_status=FAILED`
  - `adk_error_reason` 채움

### 유지해도 되는 것

- `analysis_job`을 발화 단위로 보는 설계
- `analysis_job_event`에 queue publish 이력을 저장하는 설계
- Aggregator status를 `analysis_job.status`에 저장하는 설계
- ADK status를 `analysis_result.adk_status`에 분리 저장하는 설계
- ADK metric 5개를 `analysis_metric` row로 저장하는 설계

## 9. Spring 구현 시 주의사항

### job_id 타입

Spring DB PK가 Long이어도 가능하다. 단, 현재 Go DTO는 `CreateAnalysisJobResponse.JobID string`이다. 따라서 Spring API가 다음처럼 숫자를 반환하면 현재 Go 코드와 맞지 않는다.

```json
{
  "job_id": 1
}
```

현재 Go 코드와 맞추려면 다음처럼 문자열로 반환해야 한다.

```json
{
  "job_id": "1"
}
```

또는 Go의 `JobID` 타입을 `int64`로 바꾸고 WorkerRequest/WorkerResponse/URL path 처리까지 같이 정리해야 한다.

### correlation_id

`correlation_id`는 Go가 발화마다 생성하며 emotion/STT WorkerRequest가 공유한다. Spring에서는 `analysis_job.correlation_id` unique가 적절하다. Worker event와 result에도 추적용으로 함께 저장하는 것이 좋다.

### session_id / elder_id

현재 WebSocket 연결 시 `session_id`는 Go가 UUID로 생성한다. Spring의 `conversation_session.id`가 아니다. 따라서 현재 미들웨어 기준으로 Spring job 생성 API에 `conversation_session_id`만 받는 것은 부적절하다. 지금 코드와 맞추려면 `session_id` 또는 `session_key`를 받는 것이 더 적절하다.

`elder_id`는 현재 빈 문자열이다. Spring이 Long elder PK를 기대한다면, WebSocket 인증/handshake에서 elder identity를 받아 Go의 `slowtrack.PublishUtterance()`까지 전달하는 코드가 먼저 필요하다.

결론:

```text
현재 미들웨어 기준으로 Spring job 생성 API에는 conversation_session_id를 받는 것이 부적절하다.
대신 session_key 또는 현재 Go의 session_id를 받는 것이 적절하다.
elder_id는 현재 비어 있다.
```

### partial/timeout

Aggregator는 partial/timeout final status를 만든다. 그러나 ADK 호출은 emotion 결과와 STT text가 모두 있을 때만 수행한다. Spring result 저장은 다음 경우를 모두 수용해야 한다.

- Worker 둘 다 성공, ADK 성공
- Worker 둘 다 성공, ADK 실패
- Worker 일부 실패
- Worker 일부 timeout
- Worker 전체 timeout

### ADK 실패

현재 ADK 실패는 로그만 남긴다. Spring 설계상 `analysis_job.status`와 `analysis_result.adk_status`를 분리하는 것이 맞다. 예를 들어 Worker Aggregator는 `COMPLETED`지만 ADK는 `FAILED`일 수 있다.

### result upsert

`analysis_job 1:0..1 analysis_result`라면 result 저장 API는 중복 호출에 안전해야 한다. RabbitMQ requeue, handler 재시도, 수동 재처리 가능성을 고려해 `(analysis_job_id)` unique와 upsert 정책을 권장한다.

## 10. 최종 추천안

### Spring DB 설계 최종안

권장 관계:

```text
elder 1 : N conversation_session
conversation_session 1 : N analysis_job
analysis_job 1 : N analysis_job_event
analysis_job 1 : 0..1 analysis_result
analysis_result 1 : N analysis_metric
```

권장 보강:

- `analysis_job.correlation_id` unique, not null
- `analysis_job.session_key` 또는 `conversation_session_id` 둘 중 실제 계약에 맞는 컬럼 확정
- `analysis_job_event.queue_name`, `routing_key`, `worker_type`, `event_type`, `event_status`, `payload_json`, `error_reason`, `occurred_at`
- `analysis_result.adk_status`, `adk_error_reason`, `analysis_status`, `stt_text`, emotion score fields 또는 emotion JSON
- `analysis_metric`에 `(analysis_result_id, metric_type)` unique

### Spring API 최종안

현재 Go와 최소 변경으로 맞추는 API:

```http
POST /internal/analysis-jobs
```

```json
{
  "session_id": "go-websocket-session-uuid",
  "elder_id": "",
  "correlation_id": "utterance-correlation-uuid",
  "requested_workers": ["emotion", "stt"]
}
```

응답은 현재 Go와 맞추려면 `job_id`를 문자열로 반환한다.

```json
{
  "job_id": "1",
  "status": "PENDING"
}
```

event API는 현재보다 확장하는 것을 권장한다.

```http
POST /internal/analysis-jobs/{job_id}/events
```

```json
{
  "correlation_id": "uuid",
  "worker_type": "emotion",
  "event_type": "PUBLISH_SUCCESS",
  "event_status": "SUCCESS",
  "queue_name": "todai.worker.emotion",
  "routing_key": "todai.worker.emotion",
  "message": "emotion worker request published",
  "error_reason": null,
  "payload_json": null,
  "occurred_at": "2026-06-20T22:00:01"
}
```

status API는 Go status 변환 정책을 포함해야 한다.

```http
PATCH /internal/analysis-jobs/{job_id}/status
```

```json
{
  "status": "COMPLETED",
  "correlation_id": "uuid",
  "message": "Both emotion and stt workers completed",
  "error_reason": null,
  "finished_at": "2026-06-20T22:00:10"
}
```

result 저장 API는 필요하다.

```http
POST /internal/analysis-jobs/{job_id}/result
```

ADK 성공뿐 아니라 ADK 실패, skipped, partial/timeout도 저장할 수 있어야 한다.

### Go 미들웨어에서 추가 구현해야 할 부분

- Spring result 저장 DTO 추가
- Spring result 저장 client method 추가
- `buildFinalStatusHandler()`에서 ADK 성공/실패/skipped 결과를 Backend로 저장
- status enum 변환 계층 추가
- event API 확장에 맞춰 queue name/routing key/error/payload 기록
- WebSocket에서 elder identity와 Spring conversation session 식별자를 받을 방법 추가
- Worker reply received/failed, aggregation completed/timeout, ADK requested/success/failed event 기록

## 11. 필수 질문 최종 판단

| 질문 | 판단 | 이유 |
| --- | --- | --- |
| `analysis_job 1개 = analysis_result 1개` 설계가 현재 미들웨어 구현 기준으로 맞는가? | 맞음 | Go의 Slow Track은 발화 단위로 job을 만들고, Aggregator final result와 ADK 결과도 그 job 하나에 귀속된다. 단, partial/timeout/ADK 실패에서도 result row를 만들지 여부는 정책 확정 필요 |
| `queue_name`, `routing_key`를 `analysis_job_event`에 저장하는 설계가 맞는가? | 맞음 | 하나의 job이 emotion/STT 두 큐로 fan-out된다. job row에 단일 queue를 저장하면 Worker별 publish 이력을 표현할 수 없다 |
| `POST /internal/analysis-jobs/{job_id}/result` API가 필요한가? | 맞음 | 현재 ADK 결과와 Worker 최종 결과를 Spring에 저장하는 코드가 없다. `analysis_result`, `analysis_metric`을 채우려면 result 저장 API가 필요하다 |
