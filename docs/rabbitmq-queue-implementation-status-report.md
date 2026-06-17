# RabbitMQ 큐 구현 상태 점검 리포트

## 1. 전체 요약

현재 RabbitMQ publish 최소 골격은 구현되어 있다.

- RabbitMQ connection/channel 생성 및 종료 함수
- emotion/STT 큐 선언
- JSON 직렬화 및 persistent publish
- 환경변수 기반 큐 이름 설정
- 서버 시작 시 publisher 초기화
- WebSocket 음성을 발화 단위로 묶어 두 Worker 큐에 전달

다만 단순 큐 골격을 넘어 PCM VAD, 음성 누적, 발화 종료 판단, Slow Track 실행과 3초 publish timeout까지 연결되어 있다. 이 책임들은 Handler나 queue 패키지에 직접 섞이지 않고 `orchestrator`, `slowtrack`으로 분리되어 있지만, 현재 큐 골격 확인 범위를 넘어선 동작이다.

Reply Queue, Aggregator, DB 연결, `job_id` 기반 추적은 구현되지 않았다.

## 2. 현재 구현 파일

| 파일 경로 | 역할 | 구현 상태 | 비고 |
| --- | --- | --- | --- |
| `internal/queue/client.go` | connection/channel 및 큐 선언 | 일부 구현 | 재연결 및 graceful shutdown 없음 |
| `internal/queue/topology.go` | 큐 이름과 기본값 관리 | 구현 | Reply Queue 없음 |
| `internal/queue/publisher.go` | JSON publish | 구현 | 순차 발행, confirm/retry 없음 |
| `internal/config/config.go` | RabbitMQ 환경변수 로드 | 구현 | 기본값이 topology와 중복 |
| `pkg/model/message.go` | Worker 요청/응답 DTO | 일부 구현 | DB 작업 식별자 부족 |
| `internal/slowtrack/service.go` | 요청 생성 및 publish 정책 | 일부 구현 | 3초 timeout, `ReplyTo` 미설정 |
| `internal/orchestrator/service.go` | VAD, 버퍼링, 발화 drain | 구현 | 큐 최소 범위 밖 |
| `internal/websocket/handler.go` | WebSocket 수락 및 청크 전달 | 구현 | 큐를 직접 참조하지 않음 |
| `cmd/server/main.go` | 의존성 초기화 및 조립 | 일부 구현 | 종료 신호 처리 없음 |

## 3. RabbitMQ 연결 상태

`internal/queue/client.go`의 `NewClient()`가 `amqp.Dial()`로 connection을 만들고 `conn.Channel()`로 하나의 channel을 생성한다.

연결 실패는 에러로 반환된다. `cmd/server/main.go`의 `main()`은 이를 로그로 기록하고 Slow Track을 비활성화한 채 HTTP/WebSocket 서버를 계속 실행한다.

Channel 생성 실패 시 connection을 닫고, 큐 선언 실패 시 `Client.Close()`를 호출한다.

`Client.Close()`는 channel과 connection을 순서대로 닫지만 다음 제한이 있다.

- OS signal 기반 graceful shutdown 없음
- 연결 끊김 감지 및 자동 재연결 없음
- `r.Run()` 실패 시 `log.Fatalf()`가 실행되어 등록된 `defer`가 수행되지 않음
- `/health`는 RabbitMQ 연결 여부와 무관하게 항상 정상 응답

따라서 종료 처리는 코드가 존재하지만 운영 관점에서는 일부 구현 상태다.

## 4. 큐 선언 상태

`Client.DeclareQueues()`가 다음 두 큐를 선언한다.

- `todai.worker.emotion`
- `todai.worker.stt`

두 이름은 `RABBITMQ_EMOTION_QUEUE`, `RABBITMQ_STT_QUEUE` 환경변수로 변경할 수 있다. 환경변수가 없으면 하드코딩된 기본값을 사용한다.

선언 옵션은 두 큐 모두 같다.

- `durable=true`
- `autoDelete=false`
- `exclusive=false`
- `noWait=false`
- arguments 없음

별도 exchange 선언은 없다. RabbitMQ default exchange `""`를 사용하며 큐 이름을 routing key로 지정한다.

Reply Queue `todai.reply`, dead-letter queue, retry queue는 구현되어 있지 않다.

## 5. 메시지 발행 상태

`internal/queue/publisher.go`의 `Publisher.publish()`가 payload를 `json.Marshal()`한 후 `PublishWithContext()`로 발행한다.

- `PublishEmotion()`: emotion 큐로 발행
- `PublishSTT()`: STT 큐로 발행
- `PublishToWorkers()`: emotion 성공 후 STT 순차 발행
- `ContentType`: `application/json`
- `DeliveryMode`: `amqp.Persistent`
- 실패 처리: 호출자에게 wrapping된 에러 반환
- timeout: `slowtrack.Service`에서 3초 적용

하나의 AMQP channel을 공유하므로 mutex로 publish 구간을 보호한다.

현재 `PublishToWorkers()`는 emotion publish가 실패하면 STT publish를 시도하지 않는다. Emotion 성공 후 STT 실패 시에는 부분 발행 상태가 발생할 수 있다.

Publisher confirm, mandatory return 처리, retry 및 실패 이벤트 기록은 없다.

## 6. WorkerRequest 구조

현재 `pkg/model/message.go`의 `WorkerRequest` 필드는 다음과 같다.

| 필드 | 상태 |
| --- | --- |
| `session_id` | 포함 |
| `correlation_id` | 포함, 발화마다 UUID 생성 |
| `reply_to` | 포함되나 항상 빈 문자열 |
| `audio_data` | 포함, JSON에서는 Base64 문자열로 인코딩 |
| `timestamp` | 포함, Unix millisecond |
| `elder_id` | 없음 |
| `job_id` | 없음 |
| `worker_type` 또는 작업 구분 | 없음 |

Emotion/STT 구분은 DTO 필드가 아니라 전송 대상 큐로만 이뤄진다. 두 큐에는 동일한 `correlation_id`와 payload가 전달된다.

현재 저장소에는 다음 테이블의 스키마나 DB 연동 코드가 없다.

- `analysis_job`
- `job_event_history`
- `conversation_session`
- `analysis_result`
- `analysis_metric`

따라서 실제 DB 구조와 메시지 구조의 일치 여부는 확인 필요다.

현재 구조에서 `session_id`는 `conversation_session`과 연결될 가능성이 있지만, `analysis_job`을 직접 추적할 `job_id`, 사용자 연결을 위한 `elder_id`, 작업 상태 및 재시도 정보가 없어 자연스러운 DB 연계에는 부족하다.

## 7. WebSocket Handler 연결 상태

현재 호출 흐름은 다음과 같다.

```text
WebSocket.readLoop
-> orchestrator.HandleAudioChunk
-> VAD 및 chunk 누적
-> 발화 종료 시 PublishUtterance
-> WorkerRequest 생성
-> PublishToWorkers
-> emotion 큐
-> stt 큐
```

`internal/websocket/handler.go`의 Handler는 binary 메시지를 수신해 `AudioChunkHandler`로 전달할 뿐, DTO 생성이나 RabbitMQ publish를 직접 수행하지 않는다.

Publish 단위는 chunk 단위가 아니라 발화 단위다. `orchestrator.Service`가 모든 chunk를 버퍼에 누적하고, 1.5초 침묵을 감지한 시점에 전체 버퍼를 drain하여 publish한다.

Publish 시점은 `internal/orchestrator/service.go`의 `state.vad.Observe(audioData)`가 발화 종료를 반환한 직후다.

세션 종료 시 남아 있는 미완성 버퍼는 publish되지 않고 삭제된다. 요구사항에 언급된 VAD 종료 전 STT Worker 스트리밍 전달은 구현되지 않았다.

## 8. 큐 구현 범위를 넘어선 책임

| 책임 | 위치 | 상태 |
| --- | --- | --- |
| PCM 기반 VAD | `internal/vad` | 구현, 추후 정책 분리 검토 |
| 1.5초 침묵 판단 | `vad.Detector.Observe()` | 구현, 추후 분리 필요 |
| audio chunk 누적 및 drain | `orchestrator.Service` | 구현, 추후 분리 필요 |
| Slow Track 실행 | `orchestrator`에서 `slowtrack` 호출 | 구현 |
| WorkerRequest 생성 | `slowtrack.Service` | 구현, DB 계약 확정 필요 |
| publish timeout | `slowtrack.Service` | 3초 구현 |
| Fast Track 진입 결정 | 없음 | 미구현 |
| 분석 결과 저장 | 없음 | 미구현 |
| 업무적 세션 종료 판단 | 없음 | 미구현 |
| WebSocket 종료 후 상태 삭제 | Handler/orchestrator | 구현 |

Queue 패키지 자체는 연결, 선언, 발행에 집중하고 있다. 범위를 넘어선 책임은 주로 `orchestrator`와 `slowtrack`에 존재하며, Handler와 queue 코드에서는 이미 상당 부분 분리된 상태다.

## 9. 구현 완료 / 일부 구현 / 미구현 정리

### 구현 완료

- RabbitMQ connection/channel 생성
- emotion/STT durable 큐 선언
- JSON persistent publish
- 환경변수 기반 큐 이름
- WebSocket binary chunk 수신
- VAD 기반 발화 단위 publish

### 일부 구현

- 종료 처리
- WorkerRequest DTO
- publish timeout 및 실패 로그
- session/correlation 기반 추적

### 미구현

- Reply Queue 및 consume
- Aggregator와 10초 Worker 응답 timeout
- DB 및 `analysis_job` 연결
- `job_id`, `elder_id`
- publisher confirm, retry, dead-letter queue, 재연결
- Fast Track

### 수정 필요

- 순차 publish의 부분 실패 정책
- 설정 기본값 중복과 `.env.example`의 RabbitMQ 항목 누락
- `ReplyTo` 빈 문자열
- RabbitMQ 상태를 반영하지 않는 health check
- graceful shutdown

### 책임 분리 필요

- VAD 및 발화 종료 정책의 최종 소유 계층 확정
- 발화 버퍼와 세션 생명주기 분리
- WorkerRequest 생성과 DB job 생성 순서 확정
- Slow Track timeout과 실행 정책 명시

## 10. 다음 구현 순서 제안

1. RabbitMQ 설정 기본값과 `.env.example` 정리
2. DB의 `analysis_job`, `conversation_session` 실제 스키마 확인
3. `WorkerRequest`에 `job_id`, `elder_id`, 작업 구분 필드가 필요한지 계약 확정
4. Worker별 독립 publish 및 부분 실패 정책 결정
5. publisher confirm, retry, 재연결 범위 결정
6. VAD, 발화 조립, Slow Track 실행의 책임 경계 확정
7. Reply Queue와 correlation/job 기반 Aggregator 구현
8. 10초 응답 timeout과 부분 결과 저장 구현
9. graceful shutdown 및 health check 보강

## 검증 참고

이 문서는 현재 저장소의 실제 Go 코드를 기준으로 작성했다. 코드 수정은 수행하지 않았다.

`go test ./...`는 점검 환경에 Go 실행 파일이 설치되어 있지 않아 수행하지 못했다.
