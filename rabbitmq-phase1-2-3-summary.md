# RabbitMQ 1~3차 구현 요약

## 1. 전체 요약

1~3차 구현을 통해 Go 미들웨어의 Slow Track 흐름은 다음 구조까지 확장되었다.

```text
WebSocket 발화 종료
-> Spring Backend analysis_job 생성 또는 임시 job_id 생성
-> RabbitMQ emotion/STT Worker 큐 publish
-> publish 이벤트를 Spring Backend에 기록
-> Reply Queue에서 WorkerResponse 수신
-> Aggregator가 emotion/STT 결과 결합
-> 10초 timeout 또는 완료 상태 계산
-> 최종 상태를 Spring Backend에 업데이트
```

Go 미들웨어는 DB에 직접 접근하지 않는다. DB 기록은 Spring Backend 내부 API를 통해 위임하는 방향으로 정리되었다.

## 2. 1차 구현 요약

목표는 RabbitMQ 메시지 계약과 Reply Queue 기반 구조의 기초를 만드는 것이었다.

주요 구현:

- RabbitMQ 설정 기본값을 `internal/config`로 통합
- Reply Queue `todai.reply` 설정 및 선언 추가
- `RABBITMQ_PUBLISH_TIMEOUT_SECONDS` 추가
- `WorkerRequest`에 `job_id`, `elder_id`, `worker_type` 추가
- `WorkerResponse` DTO 추가
- emotion/STT 요청을 별도 `WorkerRequest`로 생성
- 두 Worker publish를 모두 시도하는 구조로 변경
- Worker별 publish 성공/실패 로그에 `job_id`, `correlation_id` 포함

핵심 파일:

| 파일 | 역할 |
| --- | --- |
| `pkg/model/message.go` | WorkerRequest/WorkerResponse 계약 |
| `internal/queue/topology.go` | emotion/STT/reply 큐 목록 |
| `internal/queue/client.go` | 큐 선언 |
| `internal/queue/publisher.go` | Worker 큐 publish |
| `internal/slowtrack/service.go` | 발화 단위 WorkerRequest 생성 |

## 3. 2차 구현 요약

목표는 Reply Queue consumer와 메모리 기반 Aggregator를 구현하는 것이었다.

주요 구현:

- Reply Queue consumer 추가
- `WorkerResponse` JSON 파싱
- manual Ack/Nack/Reject 정책 적용
- `job_id + correlation_id` 기준 메모리 상태 관리
- emotion/STT 응답 순서와 무관하게 결과 결합
- 첫 Worker 응답 이후 10초 timeout 처리
- 중복 Worker 응답은 최초 응답 유지, 이후 응답 무시
- 완료 또는 timeout 후 Aggregator 상태 삭제
- `main.go`에서 consumer goroutine과 Aggregator 연결

처리 정책:

| 상황 | 처리 |
| --- | --- |
| 정상 JSON + handler 성공 | `Ack` |
| JSON 파싱 실패 | `Reject(requeue=false)` |
| handler 실패 | `Nack(requeue=true)` |
| 두 Worker 성공 | `completed` |
| 한 Worker 실패 | `partial_failed` |
| 두 Worker 실패 | `failed` |
| 한 Worker만 도착 후 timeout | `partial_timeout` |

핵심 파일:

| 파일 | 역할 |
| --- | --- |
| `internal/queue/consumer.go` | Reply Queue consume |
| `internal/aggregator/service.go` | 메모리 Aggregator 및 timeout |
| `cmd/server/main.go` | consumer/Aggregator lifecycle 연결 |

## 4. 3차 구현 요약

목표는 Go가 DB에 직접 접근하지 않고 Spring Backend 내부 API로 job 상태를 기록하도록 하는 것이었다.

주요 구현:

- `internal/backend` 패키지 추가
- Backend base URL 및 timeout 설정 추가
- 발화 publish 전 `analysis_job` 생성 API 호출
- Backend disabled 상태에서는 기존처럼 임시 UUID `job_id` 사용
- Backend enabled 상태에서 job 생성 실패 시 RabbitMQ publish 중단
- publish 시작/성공/실패 이벤트 기록
- Aggregator 최종 상태를 Backend status update API로 전달
- Spring Backend 내부 API 계약 문서 작성

Backend API:

| Method | Path | 역할 |
| --- | --- | --- |
| `POST` | `/internal/analysis-jobs` | analysis_job 생성 |
| `POST` | `/internal/analysis-jobs/{job_id}/events` | job event 기록 |
| `PATCH` | `/internal/analysis-jobs/{job_id}/status` | 최종 상태 업데이트 |

핵심 파일:

| 파일 | 역할 |
| --- | --- |
| `internal/backend/client.go` | Backend HTTP client |
| `internal/backend/dto.go` | Backend API DTO 및 이벤트 상수 |
| `internal/slowtrack/service.go` | job 생성 및 publish event 기록 |
| `internal/aggregator/service.go` | final status handler 호출 |
| `docs/backend-internal-api-contract.md` | Spring Backend API 계약 |

## 5. 현재 전체 흐름

```text
1. WebSocket Handler가 binary audio chunk 수신
2. orchestrator가 VAD 기반으로 발화 종료 감지
3. slowtrack.Service가 correlation_id 생성
4. Backend enabled면 Spring Backend에 analysis_job 생성 요청
5. Backend disabled면 임시 UUID job_id 생성
6. emotion/STT WorkerRequest 생성
7. RabbitMQ emotion/STT 큐로 publish
8. publish 결과 이벤트를 Backend에 기록
9. Reply Queue consumer가 WorkerResponse 수신
10. Aggregator가 job_id + correlation_id 기준으로 결과 결합
11. 두 결과 도착 또는 10초 timeout 시 최종 상태 계산
12. Backend enabled면 최종 상태 업데이트
```

## 6. 주요 환경변수

```env
RABBITMQ_URL=amqp://guest:guest@localhost:5672/
RABBITMQ_EMOTION_QUEUE=todai.worker.emotion
RABBITMQ_STT_QUEUE=todai.worker.stt
RABBITMQ_REPLY_QUEUE=todai.reply
RABBITMQ_PUBLISH_TIMEOUT_SECONDS=3

BACKEND_BASE_URL=
BACKEND_REQUEST_TIMEOUT_SECONDS=3
```

`BACKEND_BASE_URL`이 비어 있으면 Backend 연동은 비활성화된다. 이 경우 Go 미들웨어는 임시 `job_id`를 사용해 RabbitMQ publish/consume/Aggregator 흐름을 계속 수행한다.

## 7. 장애 처리 정책

| 장애 | 현재 정책 |
| --- | --- |
| RabbitMQ 연결 실패 | Slow Track publish 비활성화, WebSocket 서버는 계속 실행 |
| Backend disabled | 임시 UUID `job_id` 사용 |
| Backend job 생성 실패 | RabbitMQ publish 중단 |
| publish event 기록 실패 | 로그만 남기고 publish 결과 유지 |
| final status 업데이트 실패 | 로그만 남기고 Aggregator 완료 처리 유지 |
| WorkerResponse JSON 파싱 실패 | `Reject(requeue=false)` |
| Aggregator handler 실패 | `Nack(requeue=true)` |

## 8. 아직 구현하지 않은 것

- Spring Backend 실제 API 구현
- Go-Spring 통합 테스트
- WebSocket 연결에서 `elder_id` 전달
- 내부 API 인증/인가
- Google ADK 호출
- `analysis_result`, `analysis_metric` 저장 계약 및 구현
- Fast Track
- RabbitMQ retry/DLQ
- publisher confirm
- RabbitMQ 자동 재연결
- OS signal 기반 graceful shutdown

## 9. 테스트 상태

각 단계에서 단위 테스트는 추가되었다.

- RabbitMQ 설정 및 topology 테스트
- WorkerRequest 생성 테스트
- Reply Queue/Aggregator 정책 테스트
- Backend client HTTP 계약 테스트
- Backend enabled/disabled 및 장애 정책 테스트

다만 현재 작업 환경에는 `go`와 `gofmt` 실행 파일이 없어 `go test ./...`와 자동 포맷은 실행하지 못했다. 대신 각 단계에서 `git diff --check`와 호출 관계 검토를 수행했다.

## 10. 관련 문서

| 문서 | 내용 |
| --- | --- |
| `docs/rabbitmq-phase1-implementation-report.md` | RabbitMQ 메시지 계약 및 Reply Queue 선언 |
| `docs/rabbitmq-phase2-implementation-report.md` | Reply Queue consumer 및 Aggregator |
| `docs/rabbitmq-phase3-implementation-report.md` | Spring Backend 내부 API 연동 |
| `docs/backend-internal-api-contract.md` | Spring Backend API 계약 |
| `docs/rabbitmq-queue-implementation-status-report.md` | 초기 구현 상태 점검 |
