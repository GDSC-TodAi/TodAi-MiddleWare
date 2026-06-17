# RabbitMQ 2차 구현 결과 리포트

## 1. 변경 요약

Reply Queue에서 Python Worker 응답을 받아 메모리에서 emotion/STT 결과를 결합하는 2차 구조를 구현했다.

- Reply Queue consumer 및 전용 AMQP channel 추가
- `WorkerResponse` JSON 파싱과 manual Ack/Nack 처리
- `job_id + correlation_id` 기반 메모리 Aggregator 추가
- Emotion/STT 응답 순서와 무관한 결과 결합
- 첫 응답 이후 10초 timeout 처리
- 중복 Worker 응답 무시
- 완료 또는 timeout 후 메모리 상태 삭제
- `main.go`에서 consumer와 Aggregator lifecycle 연결

DB 저장, Fast Track, retry/DLQ, publisher confirm은 구현하지 않았다.

## 2. 변경된 파일

| 파일 경로 | 변경 내용 |
| --- | --- |
| `internal/queue/consumer.go` | Reply Queue consume, JSON 파싱, Ack/Nack/Reject 정책 구현 |
| `internal/aggregator/service.go` | 메모리 상태 관리, 결과 결합, 10초 timeout 및 최종 상태 로그 구현 |
| `internal/aggregator/service_test.go` | 응답 순서, 상태 조합, 중복, unknown type, timeout 테스트 추가 |
| `cmd/server/main.go` | Aggregator 생성, consumer goroutine 시작, context 취소 및 close 연결 |
| `docs/rabbitmq-phase2-implementation-report.md` | 2차 구현 결과 정리 |

## 3. Reply Queue Consumer 구조

`internal/queue/consumer.go`에 다음 구조를 추가했다.

```go
type ResponseHandler func(
    ctx context.Context,
    response model.WorkerResponse,
) error
```

Consumer는 publisher channel과 분리된 전용 AMQP channel을 connection에서 생성한다. Reply Queue 이름은 `Topology.ReplyQueue`를 사용한다.

`ConsumeWithContext()` 설정은 다음과 같다.

- queue: 설정된 Reply Queue
- consumer tag: `todai-reply-consumer`
- autoAck: false
- exclusive: false
- noLocal: false
- noWait: false

메시지 처리 정책은 다음과 같다.

- 정상 JSON 및 handler 성공: `Ack`
- JSON 파싱 실패: `Reject(requeue=false)`
- handler 실패: `Nack(requeue=true)`
- context 취소: consume loop 정상 종료
- 예기치 않은 delivery channel 종료: 에러 반환

JSON 파싱 실패 메시지는 다시 처리해도 성공할 가능성이 없으므로 버린다. Handler 실패는 일시적 실패일 수 있어 현재 단계에서는 requeue한다.

## 4. Aggregator 구조

`internal/aggregator/service.go`에 메모리 기반 Aggregator를 추가했다.

상태 키는 다음 함수로 생성한다.

```go
func stateKey(jobID, correlationID string) string
```

단순 구분 문자 충돌을 줄이기 위해 두 값 사이에 NUL 문자를 사용한다.

`JobState`에는 다음 정보가 저장된다.

- `job_id`
- `session_id`
- `elder_id`
- `correlation_id`
- Emotion Worker 응답
- STT Worker 응답
- 최초 응답 시각
- timeout timer
- 완료 여부

Map 접근과 timer callback은 하나의 mutex로 보호한다.

첫 Worker 응답이 도착하면 상태와 timer를 생성한다. 두 번째 Worker 응답이 도착하면 timer를 중지하고 최종 상태를 계산한 뒤 map에서 상태를 삭제한다.

동일 Worker의 중복 응답은 최초 응답을 유지하고 이후 응답을 무시한다.

## 5. WorkerResponse 처리 정책

지원하는 Worker type은 다음과 같다.

- `emotion`
- `stt`

알 수 없는 Worker type은 에러를 반환한다.

지원하는 Worker status는 다음과 같다.

- `success`
- `failed`

알 수 없는 status도 에러를 반환한다.

최종 상태 계산은 다음과 같다.

| Emotion | STT | 최종 상태 |
| --- | --- | --- |
| success | success | `completed` |
| success | failed | `partial_failed` |
| failed | success | `partial_failed` |
| failed | failed | `failed` |
| 한 Worker만 timeout 전에 도착 | 미도착 | `partial_timeout` |
| 결과가 없는 timeout | 없음 | `timeout` |

최종 로그에는 다음 값이 포함된다.

- `job_id`
- `session_id`
- `elder_id`
- `correlation_id`
- 도착한 `worker_type`
- 최종 상태

DB 연결 위치에는 다음 TODO를 남겼다.

```go
// TODO: persist final job status to analysis_job and job_event_history.
```

## 6. Timeout 처리 방식

운영 기본 timeout은 `10초`다.

Aggregator는 첫 Worker 응답이 도착한 시점에 `time.AfterFunc()` timer를 시작한다.

10초 안에 두 응답이 모두 도착하면 다음 순서로 처리한다.

1. mutex 획득
2. 두 응답 존재 확인
3. 완료 표시
4. timer 중지
5. map에서 상태 삭제
6. mutex 해제
7. 최종 상태 로그 출력

Timeout이 먼저 실행되면 다음 순서로 처리한다.

1. mutex 획득
2. 상태가 아직 활성 상태인지 확인
3. 완료 표시
4. map에서 상태 삭제
5. mutex 해제
6. `partial_timeout` 또는 `timeout` 로그 출력

테스트에서는 timeout duration을 주입해 실제 10초를 기다리지 않는다.

## 7. main.go 연결 흐름

RabbitMQ 연결 성공 시 흐름은 다음과 같다.

```text
RabbitMQ Client 생성
-> Aggregator 생성
-> Reply Consumer 전용 channel 생성
-> consumer context 생성
-> ConsumeReplies goroutine 시작
-> WorkerResponse를 Aggregator.HandleWorkerResponse로 전달
-> 기존 Slow Track publisher 생성
```

서버 종료 시 defer는 다음 순서로 실행된다.

1. consumer context 취소
2. consumer channel close
3. Aggregator timer/state 정리
4. RabbitMQ publisher channel 및 connection close

Reply consumer channel 생성이 실패해도 기존 publisher와 WebSocket 서버는 계속 동작한다.

## 8. 아직 구현하지 않은 것

- `analysis_job` 생성 및 저장
- `job_event_history` 저장
- `analysis_result` 저장
- `analysis_metric` 저장
- DB repository
- Google ADK 호출
- Fast Track
- Python Worker 구현
- retry 및 dead-letter queue
- publisher confirm
- RabbitMQ 자동 재연결
- consumer prefetch 최적화
- 여러 consumer group
- OS signal 기반 전체 graceful shutdown

## 9. 테스트 결과

다음 Aggregator 단위 테스트를 추가했다.

- Emotion 응답만 도착했을 때 상태 생성
- Emotion 이후 STT 도착 시 completed
- STT 이후 Emotion 도착 시 completed
- success/failed 조합의 partial_failed
- failed/failed 조합의 failed
- unknown worker type 에러
- 중복 Worker 응답 무시
- 짧은 테스트 timeout 이후 partial_timeout 및 상태 삭제

실행 명령:

```bash
go test ./...
```

현재 작업 환경에는 `go`와 `gofmt` 실행 파일이 없어 테스트와 자동 포맷을 실행하지 못했다.

대신 다음 정적 검증을 수행했다.

- `git diff --check`
- Go import 및 호출 관계 수동 검토
- `amqp091-go v1.10.0`의 `ConsumeWithContext` API 확인
- DB, Fast Track, retry/DLQ 코드가 추가되지 않았는지 검색

## 10. 다음 구현 제안

1. Python Worker가 `WorkerResponse` 계약을 정확히 반환하는지 통합 테스트
2. RabbitMQ를 포함한 consumer Ack/Nack 통합 테스트 추가
3. `analysis_job` 생성 시점과 상태 전이 정의
4. Aggregator 최종 결과를 저장 계층으로 전달하는 인터페이스 추가
5. `job_event_history` 이벤트 종류 정의
6. 10초 timeout의 부분 결과를 ADK에 전달할지 정책 확정
7. late response 및 완료 후 중복 response 처리 보존 기간 정의
8. retry, DLQ, publisher confirm 및 재연결 정책 설계
