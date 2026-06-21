# 큐 중심 현재 구현 상황 및 다음 작업 정리

## 1. 요약

현재 Go 미들웨어의 큐 관련 구현은 단순 publish 골격을 넘어, 발화 단위 Slow Track 작업 생성부터 RabbitMQ Worker A/B fan-out, Reply Queue consume, Aggregator 결합, Spring Backend job 상태 업데이트, ADK 호출까지 연결되어 있다.

다만 아직 운영 가능한 전체 분석 저장 파이프라인으로 보기는 어렵다. ADK 호출 결과는 로그로만 남고, `analysis_result`, `analysis_metric` 저장 계약과 구현이 없다. RabbitMQ retry/DLQ, publisher confirm, 자동 재연결, health check 반영도 아직 남아 있다.

현재 상태를 한 줄로 정리하면 다음과 같다.

```text
WebSocket audio chunk
-> VAD 발화 종료 감지
-> Fast Track goroutine
-> Slow Track goroutine
-> Spring Backend analysis_job 생성 또는 임시 job_id 생성
-> RabbitMQ emotion/STT 큐 publish
-> Reply Queue에서 WorkerResponse consume
-> Aggregator가 emotion/STT 결과 결합 또는 timeout 처리
-> Spring Backend job status 업데이트
-> ADK /analyze 호출
-> ADK 결과는 로그만 출력
```

## 2. 현재 구현된 큐 흐름

### 2.1 서버 초기화

`cmd/server/main.go`에서 서버 시작 시 다음 객체를 만든다.

| 구성 요소 | 현재 역할 |
| --- | --- |
| `backend.Client` | Spring Backend 내부 API 호출 |
| `adk.Client` | ADK `/analyze` 호출 |
| `queue.Client` | RabbitMQ connection/channel 생성 및 queue declare |
| `queue.Publisher` | Worker A/B 큐 publish |
| `queue.Consumer` | Reply Queue consume |
| `aggregator.Service` | Worker 응답 결합 및 timeout 처리 |
| `slowtrack.Service` | job 생성, WorkerRequest 구성, publish 실행 |

RabbitMQ 연결에 실패하면 Slow Track publish는 비활성화되지만, HTTP/WebSocket 서버는 계속 뜬다.

### 2.2 큐 topology

현재 큐 이름은 `internal/config`에서 환경변수로 읽고, 기본값은 다음과 같다.

| 환경변수 | 기본값 | 역할 |
| --- | --- | --- |
| `RABBITMQ_EMOTION_QUEUE` | `todai.worker.emotion` | Worker A 감정 분석 요청 큐 |
| `RABBITMQ_STT_QUEUE` | `todai.worker.stt` | Worker B 정밀 STT 요청 큐 |
| `RABBITMQ_REPLY_QUEUE` | `todai.reply` | Python Worker 응답 수신 큐 |

`internal/queue/topology.go`의 `Queues()`는 emotion, STT, reply 세 큐를 모두 반환한다. `internal/queue/client.go`는 이 큐들을 durable queue로 선언한다.

현재 별도 exchange는 선언하지 않는다. publish는 RabbitMQ default exchange `""`를 사용하고, queue name을 routing key로 지정한다.

### 2.3 Slow Track publish

`internal/orchestrator/service.go`는 VAD가 발화 종료를 감지하면 발화 버퍼를 복사한 뒤 Fast Track과 Slow Track을 각각 goroutine으로 실행한다.

Slow Track 쪽은 `internal/slowtrack/service.go`가 담당한다.

1. `correlation_id`를 새 UUID로 생성한다.
2. 현재 WebSocket에서 elder identity를 받지 않으므로 `elder_id`는 빈 문자열이다.
3. Backend 연동이 켜져 있으면 `POST /internal/analysis-jobs`로 job을 먼저 생성한다.
4. Backend 연동이 꺼져 있으면 임시 UUID를 `job_id`로 사용한다.
5. `WorkerRequest` 두 개를 만든다.
6. emotion 요청에는 `worker_type=emotion`, STT 요청에는 `worker_type=stt`를 넣는다.
7. 두 요청 모두 `reply_to`에 설정된 Reply Queue 이름을 넣는다.
8. `queue.Publisher.PublishToWorkers()`로 두 Worker 큐에 publish한다.

publish timeout은 `RABBITMQ_PUBLISH_TIMEOUT_SECONDS`로 설정하며 기본값은 3초다.

### 2.4 WorkerRequest 계약

현재 Worker 요청 구조는 `pkg/model/message.go`의 `WorkerRequest`다.

```go
type WorkerRequest struct {
    JobID         string `json:"job_id"`
    SessionID     string `json:"session_id"`
    ElderID       string `json:"elder_id"`
    CorrelationID string `json:"correlation_id"`
    WorkerType    string `json:"worker_type"`
    ReplyTo       string `json:"reply_to"`
    AudioData     []byte `json:"audio_data"`
    Timestamp     int64  `json:"timestamp"`
}
```

Python Worker는 이 요청을 처리한 뒤 `reply_to` 큐로 `WorkerResponse`를 보내야 한다.

### 2.5 publish 정책

`internal/queue/publisher.go`는 emotion publish와 STT publish를 독립적으로 시도한다. emotion publish가 실패해도 STT publish를 계속 시도한다.

현재 적용된 RabbitMQ publish 속성은 다음과 같다.

| 항목 | 현재 값 |
| --- | --- |
| exchange | `""` |
| routing key | queue name |
| content type | `application/json` |
| delivery mode | persistent |
| mandatory | `false` |
| immediate | `false` |

아직 publisher confirm은 사용하지 않는다. 즉 `PublishWithContext()` 호출 성공은 client/channel 레벨 publish 성공을 의미하지만, broker가 메시지를 확정적으로 수락했는지 확인하는 단계는 없다.

### 2.6 Reply Queue consume

`internal/queue/consumer.go`는 Reply Queue에서 `WorkerResponse` JSON을 consume한다.

현재 Ack/Nack 정책은 다음과 같다.

| 상황 | 처리 |
| --- | --- |
| JSON unmarshal 실패 | `Reject(false)`로 폐기 |
| Aggregator handler 실패 | `Nack(false, true)`로 requeue |
| 정상 처리 | `Ack(false)` |
| context 종료 | consume loop 정상 종료 |

consumer tag는 `todai-reply-consumer`다.

### 2.7 Aggregator

`internal/aggregator/service.go`는 `job_id + correlation_id`를 key로 메모리 상태를 관리한다.

현재 동작은 다음과 같다.

1. 첫 Worker 응답이 오면 Aggregator state를 만든다.
2. 첫 응답 도착 시점부터 10초 timer를 시작한다.
3. emotion/STT 응답을 각각 한 번씩만 저장한다.
4. 중복 응답은 무시한다.
5. 두 Worker 응답이 모두 오면 timer를 멈추고 state를 삭제한다.
6. timeout이 먼저 오면 도착한 결과만으로 final result를 만들고 state를 삭제한다.

최종 status는 다음 중 하나다.

| status | 의미 |
| --- | --- |
| `completed` | emotion/STT 둘 다 성공 |
| `partial_failed` | 둘 중 하나 실패 |
| `failed` | 둘 다 실패 |
| `partial_timeout` | timeout 전 일부 응답만 도착 |
| `timeout` | timeout 전 아무 응답도 도착하지 않음 |

성공한 emotion 결과는 `model.EmotionResult`로 파싱하고, 성공한 STT 결과는 `model.STTResult.Text`로 파싱해 `FinalResult`에 담는다.

## 3. ADK 연동 현재 상태

현재 ADK 호출 코드는 구현되어 있다.

`cmd/server/main.go`의 `buildFinalStatusHandler()`는 Aggregator final result를 받은 뒤 다음을 수행한다.

1. Backend가 켜져 있으면 job status를 업데이트한다.
2. ADK가 켜져 있고, emotion 결과와 STT text가 모두 있으면 ADK `/analyze`를 호출한다.

ADK client 계약은 `internal/adk/client.go` 기준으로 다음과 같다.

```text
POST {ADK_BASE_URL}/analyze
Content-Type: application/json

{
  "emotion": {
    "sadness": 0.0,
    "anxiety": 0.0,
    "neutral": 0.0,
    "joy": 0.0
  },
  "text": "..."
}
```

응답은 다음 5가지 지표를 기대한다.

| 필드 | 의미 |
| --- | --- |
| `social_isolation` | 사회적 고립 |
| `health_anxiety` | 건강 불안 |
| `daily_vitality` | 일상 활력 |
| `emotion_variance` | 감정 변동 |
| `cognitive_load` | 인지 부하 |

중요한 제한점은 ADK 결과를 현재 로그로만 출력한다는 점이다. DB 저장이나 Backend 전달 API는 아직 없다.

## 4. Spring Backend 연동 현재 상태

현재 Go 미들웨어가 호출하는 Backend 내부 API는 세 가지다.

| 메서드 | 경로 | 호출 시점 |
| --- | --- | --- |
| `POST` | `/internal/analysis-jobs` | Worker publish 전 job 생성 |
| `POST` | `/internal/analysis-jobs/{job_id}/events` | publish 시작/성공/실패 event 기록 |
| `PATCH` | `/internal/analysis-jobs/{job_id}/status` | Aggregator final status 업데이트 |

현재 Backend DTO에는 ADK 결과나 Worker 원본 결과를 저장하는 요청 구조가 없다.

즉 현재 Backend 연동은 작업 추적과 상태 업데이트 중심이며, 분석 결과 저장까지 확장되어 있지는 않다.

## 5. 현재 구현된 것

큐 관련해서 구현된 것은 다음과 같다.

- RabbitMQ URL, emotion/STT/reply queue, publish timeout 설정
- RabbitMQ connection 및 publisher channel 생성
- emotion/STT/reply durable queue 선언
- WebSocket 음성 청크 수신
- VAD 기반 발화 종료 감지
- 발화 단위 Slow Track publish
- Worker별 `WorkerRequest` 생성
- `job_id`, `correlation_id`, `worker_type`, `reply_to` 포함
- Spring Backend job 생성
- Spring Backend publish event 기록
- emotion/STT Worker 큐 독립 publish 시도
- Reply Queue consumer
- `WorkerResponse` JSON decode
- Ack/Nack/Reject 기본 정책
- 메모리 Aggregator
- 10초 Worker 응답 timeout
- partial timeout/failed status 처리
- Aggregator final status Backend 업데이트
- ADK `/analyze` 호출
- Fast Track과 Slow Track fan-out 실행 구조

## 6. 아직 해야 할 것

### 6.1 분석 결과 저장 계약 확정

가장 먼저 정해야 할 것은 ADK 결과를 어디에, 어떤 API로 저장할지다.

현재 필요한 계약은 다음 중 하나다.

1. Go가 Spring Backend 내부 API로 분석 결과 저장 요청을 보낸다.
2. Go가 DB에 직접 저장한다.

프로젝트 원칙상 현재 문서와 코드 흐름은 1번, 즉 Spring Backend 내부 API 위임 방향으로 정리되어 있다. 따라서 다음 API 계약이 필요하다.

```text
POST /internal/analysis-jobs/{job_id}/result
```

또는 result와 metric을 분리한다면 다음처럼 나눌 수 있다.

```text
POST /internal/analysis-jobs/{job_id}/result
POST /internal/analysis-jobs/{job_id}/metrics
```

저장해야 할 최소 데이터는 다음과 같다.

| 데이터 | 출처 |
| --- | --- |
| `job_id` | Backend 또는 Go 임시 생성 |
| `session_id` | WebSocket session |
| `elder_id` | 추후 WebSocket identity |
| `correlation_id` | Slow Track job 생성 시 UUID |
| emotion distribution | Worker A |
| standard text | Worker B |
| 5 metrics | ADK |
| final status | Aggregator |
| error/partial/timeout reason | Aggregator 및 Worker response |

### 6.2 ADK 결과 저장 구현

ADK 호출은 되어 있지만 결과는 로그로만 남는다.

다음 작업이 필요하다.

1. `internal/backend/dto.go`에 분석 결과 저장 DTO 추가
2. `internal/backend/client.go`에 결과 저장 API method 추가
3. `buildFinalStatusHandler()`에서 ADK 성공 후 Backend 저장 API 호출
4. ADK 실패 시 job status를 어떻게 둘지 정책 결정
5. 부분 결과일 때 ADK를 호출할지 정책 결정

현재 코드는 emotion 결과와 STT text가 모두 있어야 ADK를 호출한다. 따라서 `partial_timeout`, `partial_failed` 상황에서는 둘 중 하나라도 없으면 ADK 호출이 생략된다.

### 6.3 WorkerResponse 계약 검증

Python Worker가 반드시 현재 Go 계약에 맞춰 응답해야 한다.

현재 Go가 기대하는 status 값은 다음 두 개뿐이다.

```text
success
failed
```

초기 문서의 `ok`, `error`, `partial`과 다르다. Worker 팀과 최종 계약을 맞춰야 한다.

현재 Go가 기대하는 result 형태는 다음과 같다.

emotion:

```json
{
  "sadness": 0.1,
  "anxiety": 0.2,
  "neutral": 0.6,
  "joy": 0.1
}
```

stt:

```json
{
  "text": "오늘은 몸이 조금 무겁지만 괜찮아요."
}
```

### 6.4 Reply Queue 안정성 보강

현재 consumer는 handler 실패 시 무한 requeue될 수 있다. 운영 기준으로는 다음이 필요하다.

- retry 횟수 제한
- DLQ 설정
- poison message 분리
- worker response schema validation 실패 시 관측 가능한 event 기록
- requeue storm 방지

### 6.5 Publisher confirm 적용

현재 publish는 persistent message와 durable queue를 사용하지만 publisher confirm은 없다.

필요 작업은 다음과 같다.

- channel confirm mode 활성화
- publish 후 broker confirm 대기
- confirm timeout 정책 추가
- confirm 실패 시 job event 기록
- emotion/STT 각각의 publish 확정 상태 저장

### 6.6 RabbitMQ 자동 재연결

현재 RabbitMQ 연결이 서버 시작 시 실패하면 Slow Track이 비활성화된다. 서버 실행 중 연결이 끊겼을 때 자동 복구하는 구조는 없다.

필요 작업은 다음과 같다.

- connection/channel close notification 감지
- 재연결 loop
- topology 재선언
- publisher/consumer 재생성
- 재연결 중 publish 요청 처리 정책 결정

### 6.7 health check 확장

현재 `/health`는 단순히 `{"status":"ok"}`를 반환한다.

운영용으로는 다음 상태를 분리하는 것이 좋다.

- HTTP 서버 상태
- RabbitMQ 연결 상태
- Reply consumer 동작 상태
- Backend 연동 상태
- ADK 연동 상태

예시는 다음과 같다.

```json
{
  "status": "degraded",
  "rabbitmq": "down",
  "reply_consumer": "stopped",
  "backend": "enabled",
  "adk": "enabled"
}
```

### 6.8 WebSocket identity 연결

현재 `elder_id`는 빈 문자열이다.

분석 결과를 사용자/어르신 단위로 저장하려면 WebSocket 연결 시점에 elder identity를 받아서 Slow Track까지 전달해야 한다.

필요 작업은 다음과 같다.

- WebSocket 인증 또는 query/header 기반 identity 전달 방식 결정
- `websocket.Session`에 elder ID 보관
- `orchestrator.HandleAudioChunk()` 호출 경로에 elder ID 포함
- `slowtrack.PublishUtterance()`에 elder ID 전달
- WorkerRequest와 Backend job 생성 요청에 elder ID 채우기

### 6.9 graceful shutdown 정리

현재 RabbitMQ client와 consumer close는 `defer`로 처리된다. 그러나 OS signal 기반 graceful shutdown과 진행 중인 goroutine 대기는 명시적으로 구현되어 있지 않다.

필요 작업은 다음과 같다.

- `http.Server` 직접 생성
- SIGINT/SIGTERM 처리
- root context cancel
- reply consumer 종료 대기
- in-flight publish/ADK/backend 요청 timeout 정리

### 6.10 테스트 보강

현재 단위 테스트는 일부 존재하지만, 큐 전체 흐름의 통합 검증은 더 필요하다.

우선순위 높은 테스트는 다음과 같다.

- RabbitMQ test container 기반 publish/consume 통합 테스트
- WorkerResponse schema mismatch 테스트
- Aggregator가 ADK 호출 조건을 만족하는지 테스트
- ADK 성공 후 Backend 저장 API 호출 테스트
- ADK 실패 시 status/event 정책 테스트
- RabbitMQ handler 실패 requeue 정책 테스트
- Backend disabled 상태에서 ADK 호출/저장 정책 테스트

## 7. 추천 작업 순서

큐 관련 다음 작업은 아래 순서가 가장 자연스럽다.

1. Python Worker와 `WorkerResponse` 최종 계약 확정
2. Spring Backend와 분석 결과 저장 API 계약 확정
3. ADK 결과 저장 DTO 및 Backend client method 추가
4. `buildFinalStatusHandler()`에서 ADK 결과 저장까지 연결
5. 부분 실패/timeout 시 ADK 호출 및 저장 정책 확정
6. Reply Queue retry/DLQ 정책 추가
7. publisher confirm 적용
8. RabbitMQ 자동 재연결 추가
9. health check에 RabbitMQ/consumer/ADK 상태 반영
10. WebSocket elder identity 전달
11. RabbitMQ 포함 통합 테스트 추가

## 8. 현재 가장 중요한 결론

큐 파이프라인 자체는 이제 `publish -> reply consume -> aggregate -> ADK call`까지 이어져 있다.

하지만 서비스 관점의 완료 기준은 아직 충족하지 못했다. 가장 큰 빈칸은 ADK 결과 저장이다. 지금은 ADK가 5가지 지표를 반환해도 로그에만 찍히므로, Spring Backend와 `analysis_result`, `analysis_metric` 저장 계약을 먼저 확정하고 그 API를 Go 미들웨어에 연결하는 것이 다음 핵심 작업이다.
