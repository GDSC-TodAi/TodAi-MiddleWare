# RabbitMQ 큐 구현 리포트

## 1. 구현 배경

TodAi Go 미들웨어의 Slow Track 분석 작업을 RabbitMQ를 통해 Worker A와 Worker B에 전달하기 위한 큐 publish 구조를 추가했다.

현재 단계의 목표는 Reply Queue 수집, Aggregator, ADK 분석, DB 저장까지 포함한 전체 분석 파이프라인을 완성하는 것이 아니다. RabbitMQ 연결과 채널 생성, Worker 큐 선언, WebSocket으로 수신한 음성 데이터를 Worker A/B 큐에 publish하는 최소 골격을 마련하는 것이 목표다.

## 2. 현재 구현 범위

현재 구현된 범위는 다음과 같다.

- RabbitMQ URL을 이용한 connection 생성
- connection에서 publish용 channel 생성 및 관리
- emotion/stt Worker 큐 선언
  - durable queue로 선언
  - auto-delete 및 exclusive 옵션은 비활성화
- default exchange를 이용한 큐 이름 기반 routing
- payload를 JSON으로 marshal하여 전송
- publish 메시지의 `Content-Type`을 `application/json`으로 설정
- publish 메시지를 persistent delivery mode로 설정
- 환경변수를 통한 RabbitMQ URL 및 emotion/stt 큐 이름 관리
- 환경변수가 없을 때 기본 URL 및 큐 이름 사용
- 서버 시작 시 RabbitMQ client와 publisher 초기화
- 서버 종료 시 RabbitMQ channel 및 connection close 처리
- WebSocket binary audio chunk를 Slow Track 서비스로 전달
- Slow Track 서비스에서 audio chunk를 `WorkerRequest`로 감싸 Worker A/B 큐에 publish
- publish 작업별 3초 timeout 및 비동기 실행

현재 RabbitMQ 연결에 실패하더라도 HTTP/WebSocket 서버는 실행된다. 이 경우 Slow Track publisher가 비활성화되며, WebSocket으로 수신한 audio chunk는 큐에 publish되지 않는다.

## 3. 주요 파일별 역할

### `internal/queue/client.go`

- RabbitMQ connection과 channel을 생성한다.
- `Topology.WorkerQueues()`에 등록된 emotion/stt 큐를 선언한다.
- 큐를 durable로 선언한다.
- 초기화 도중 channel 생성 또는 큐 선언이 실패하면 이미 생성한 자원을 정리한다.
- `Close`에서 channel과 connection을 순서대로 종료한다.

### `internal/queue/topology.go`

- RabbitMQ URL과 Worker 큐 이름의 기본값을 정의한다.
- emotion/stt 큐 이름을 담는 `Topology`를 제공한다.
- 설정값이 비어 있으면 기본 큐 이름을 적용한다.
- 선언 대상 Worker 큐 목록을 `WorkerQueues()`로 제공한다.

기본값은 다음과 같다.

- RabbitMQ URL: `amqp://guest:guest@localhost:5672/`
- Emotion Queue: `todai.worker.emotion`
- STT Queue: `todai.worker.stt`

### `internal/queue/publisher.go`

- payload를 JSON으로 marshal한 뒤 Worker 큐에 publish한다.
- `PublishEmotion`은 emotion Worker 큐에 publish한다.
- `PublishSTT`는 STT Worker 큐에 publish한다.
- `PublishToWorkers`는 동일한 payload를 emotion 큐와 STT 큐에 순차 publish한다.
- default exchange를 사용하고 큐 이름을 routing key로 지정한다.
- 메시지에 `application/json` Content-Type과 persistent delivery mode를 설정한다.
- 하나의 AMQP channel을 여러 goroutine에서 사용할 수 있도록 mutex로 publish 구간을 보호한다.

현재 `PublishToWorkers`는 emotion publish가 실패하면 STT publish를 시도하지 않는다. 두 Worker로의 독립적인 전달 보장이나 부분 실패 처리 정책은 추후 개선 범위다.

### `internal/config/config.go`

- 다음 RabbitMQ 관련 환경변수를 로드한다.
  - `RABBITMQ_URL`
  - `RABBITMQ_EMOTION_QUEUE`
  - `RABBITMQ_STT_QUEUE`
- 환경변수가 없으면 로컬 RabbitMQ URL과 기본 Worker 큐 이름을 사용한다.

### `cmd/server/main.go`

- `.env`와 애플리케이션 설정을 로드한다.
- 설정값으로 RabbitMQ topology와 client를 초기화한다.
- RabbitMQ client로 publisher를 생성한다.
- publisher를 `slowtrack.Service`에 주입하고, 해당 서비스를 WebSocket Handler의 `AudioChunkHandler`로 주입한다.
- 서버 종료 시 RabbitMQ client의 `Close`를 호출한다.
- RabbitMQ 초기화가 실패하면 Slow Track publish를 비활성화한 상태로 서버를 계속 실행한다.

### `pkg/model/message.go`

- Go 미들웨어가 Python Worker에 보내는 `WorkerRequest` 구조체를 정의한다.
- `WorkerRequest`에는 session ID, correlation ID, reply queue 정보, audio data, timestamp가 포함된다.
- `WorkerReply`, `EmotionResult`, `STTResult`도 정의되어 있으나, 현재 단계에서는 Reply Queue와 Aggregator가 구현되지 않아 실제 수신 처리에는 사용되지 않는다.
- 현재 `WorkerRequest.ReplyTo`는 빈 문자열로 publish된다.

### `internal/websocket/handler.go`

- HTTP 요청을 WebSocket 연결로 upgrade한다.
- 연결별 session ID를 생성하고 활성 세션을 관리한다.
- binary WebSocket 메시지를 audio chunk로 수신한다.
- 수신한 audio chunk를 주입된 `AudioChunkHandler`에 전달하여 Slow Track publish 흐름에 연결한다.
- binary가 아닌 메시지는 무시한다.

현재 Handler는 VAD, chunk 누적, `WorkerRequest` 생성, RabbitMQ publish 실행 정책을 직접 담당하지 않는다. 이 책임은 Handler 외부로 분리되어 있으며, 현재 `internal/slowtrack/service.go`가 `WorkerRequest` 생성과 publish 실행 정책을 담당한다.

### `internal/slowtrack/service.go`

- WebSocket Handler에서 전달받은 audio chunk를 복사한다.
- session ID, 신규 correlation ID, timestamp를 포함한 `WorkerRequest`를 생성한다.
- 현재 Reply Queue가 없으므로 `ReplyTo`를 빈 문자열로 설정한다.
- 별도 goroutine에서 `PublishToWorkers`를 호출한다.
- 각 publish 작업에 3초 timeout을 적용하고 결과를 로그로 남긴다.

현재는 VAD와 발화 단위 조립이 연결되지 않았기 때문에, WebSocket에서 수신한 각 binary audio chunk가 하나의 `WorkerRequest`로 취급되어 즉시 publish된다.

## 4. Queue 구조

| Queue Name | Direction | Description |
|---|---|---|
| `todai.worker.emotion` | Go → Worker A | 감정 분석 Worker 요청 |
| `todai.worker.stt` | Go → Worker B | STT 및 표준어 변환 Worker 요청 |

두 큐 이름은 환경변수로 변경할 수 있다. 현재 단계에서는 Python Worker 응답을 받기 위한 Reply Queue인 `todai.reply`를 선언하거나 consume하는 기능은 구현하지 않았다.

## 5. Publish 흐름

현재 메시지 흐름은 다음과 같다.

1. 클라이언트가 WebSocket binary audio chunk를 전송한다.
2. WebSocket Handler가 binary 메시지를 수신하고 session ID와 audio data를 `slowtrack.Service`에 전달한다.
3. Slow Track 서비스가 audio chunk를 복사하고 `WorkerRequest`를 생성한다.
4. Slow Track 서비스가 별도 goroutine에서 `PublishToWorkers`를 호출한다.
5. Publisher가 요청을 JSON으로 변환한다.
6. Publisher가 `todai.worker.emotion` 큐에 먼저 publish한다.
7. emotion publish가 성공하면 `todai.worker.stt` 큐에 publish한다.

```text
Client
  |
  | WebSocket binary audio chunk
  v
WebSocket Handler
  |
  | session ID + audio chunk
  v
Slow Track Service
  |
  | WorkerRequest 생성 및 PublishToWorkers 호출
  v
RabbitMQ default exchange
  |-- todai.worker.emotion --> Worker A
  `-- todai.worker.stt     --> Worker B
```

현재 publish 단위는 완성된 발화가 아니라 WebSocket binary audio chunk다. 따라서 클라이언트가 하나의 발화를 여러 chunk로 전송하면 각 chunk마다 서로 다른 correlation ID를 가진 요청이 두 Worker 큐로 전달된다.

## 6. 아직 구현하지 않은 범위

다음 항목은 현재 RabbitMQ publish 최소 골격에 포함되지 않는다.

- Reply Queue인 `todai.reply` 선언 및 consume
- `WorkerReply` 역직렬화와 correlation ID 기반 응답 매칭
- Worker A/B 응답을 모으는 Aggregator 및 Late Fusion
- Worker 응답 대기용 10초 timeout과 부분 결과 저장
- Google ADK 연동 및 5가지 지표 분석
- 분석 결과 DB 저장
- Worker publish confirm, return 처리, retry, dead-letter queue
- RabbitMQ 연결 및 channel 자동 복구
- VAD 기반 발화 종료 감지
- 여러 audio chunk를 하나의 발화 데이터로 누적 및 조립
- Slow Track Worker로의 STT 선처리 스트리밍
- Fast Track과 Slow Track의 실제 fan-out 오케스트레이션

`WorkerReply`와 결과 구조체는 데이터 모델로 정의되어 있지만, 이를 사용하는 수신 파이프라인은 아직 없다.

## 7. 추후 분리 및 개선 필요 사항

### 발화 단위 publish로 전환

현재 `slowtrack.Service.HandleAudioChunk`는 수신한 chunk마다 즉시 `WorkerRequest`를 생성한다. VAD와 발화 조립이 구현되면 입력 단위를 audio chunk가 아닌 완성된 발화 데이터로 변경해야 한다.

WebSocket Handler는 연결과 메시지 수신 역할을 유지하고, VAD 및 발화 조립 정책은 별도 계층에서 담당하는 구조가 적절하다.

### Worker별 publish 실패 처리

현재 두 큐 publish는 순차 실행되며 emotion publish 실패 시 STT publish가 생략된다. Worker별 publish를 독립적으로 시도할지, 일부 publish 실패를 어떻게 기록하고 재시도할지 정책을 정해야 한다.

### RabbitMQ 신뢰성 및 복구

persistent 메시지와 durable 큐는 적용되어 있지만 publisher confirm은 사용하지 않는다. RabbitMQ가 메시지를 실제로 수락했는지 확인하는 처리, 연결 끊김 시 재연결, retry 및 dead-letter 정책이 추후 필요하다.

### 종료 처리

현재 RabbitMQ client는 `main`의 `defer`로 정리된다. 명시적인 OS signal 기반 graceful shutdown과 진행 중인 publish 작업 종료 대기는 아직 구현되지 않았다.

### Reply Queue 및 Aggregator 연계

Reply Queue가 구현되면 `WorkerRequest.ReplyTo`에 실제 reply queue 이름을 설정하고, correlation ID를 기준으로 Worker A/B 응답을 수집해야 한다. 응답 대기에는 프로젝트 정책인 최대 10초 timeout을 적용하고, 일부 Worker가 timeout되어도 도착한 결과만으로 부분 저장할 수 있어야 한다.
