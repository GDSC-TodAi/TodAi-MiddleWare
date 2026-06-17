# RabbitMQ 1차 구현 결과 리포트

## 1. 변경 요약

RabbitMQ 메시지 계약과 향후 Reply Queue consumer/Aggregator를 연결할 수 있는 1차 구조를 구현했다.

- RabbitMQ 설정 기본값을 `internal/config`로 통합
- Reply Queue `todai.reply` 설정 및 선언 추가
- publish timeout 환경변수 추가
- `WorkerRequest`에 job, elder, worker 식별 정보 추가
- Reply Queue 응답 계약인 `WorkerResponse` 추가
- emotion/STT 요청을 별도 DTO로 생성
- 두 Worker publish를 모두 시도하고 부분 실패를 결합해 반환
- Worker별 publish 성공/실패 로그에 `job_id`, `correlation_id` 추가
- 서버 시작 실패 시 RabbitMQ `defer Close()`가 실행되도록 종료 처리 최소 보강

Aggregator, Reply Queue consume, DB 저장은 이번 범위에 포함하지 않았다.

## 2. 변경된 파일

| 파일 경로 | 변경 내용 |
| --- | --- |
| `.env.example` | RabbitMQ URL, Worker 큐, Reply Queue, publish timeout 예시 추가 |
| `internal/config/config.go` | RabbitMQ 기본값 통합, Reply Queue 및 timeout 설정 추가 |
| `internal/config/config_test.go` | 설정 기본값, override, 잘못된 timeout fallback 테스트 추가 |
| `internal/queue/topology.go` | Reply Queue 필드와 전체 큐 목록 추가 |
| `internal/queue/topology_test.go` | Reply Queue가 선언 목록에 포함되는지 검증 |
| `internal/queue/client.go` | emotion/STT/Reply Queue를 모두 선언하도록 변경 |
| `internal/queue/publisher.go` | typed 요청, worker type 검증, 독립 publish 및 결합 에러 구현 |
| `internal/queue/publisher_test.go` | 두 Worker 검증 실패가 함께 반환되는지 검증 |
| `pkg/model/message.go` | WorkerRequest 확장, WorkerResponse 및 상수 추가 |
| `internal/slowtrack/service.go` | job/request 생성과 Reply Queue 연결, Worker별 요청 생성 |
| `internal/slowtrack/service_test.go` | 두 요청의 공유 ID와 worker type 계약 검증 |
| `cmd/server/main.go` | Reply Queue topology, timeout 주입, 종료 처리 최소 보강 |

## 3. 메시지 구조 변경

`WorkerRequest`에 다음 필드를 추가했다.

- `job_id`
- `elder_id`
- `worker_type`

Worker type 상수는 다음과 같다.

- `WorkerTypeEmotion = "emotion"`
- `WorkerTypeSTT = "stt"`

발화마다 임시 `job_id`와 `correlation_id`를 UUID로 생성한다. Emotion/STT 요청은 다음 값을 공유한다.

- `job_id`
- `session_id`
- `elder_id`
- `correlation_id`
- `reply_to`
- `audio_data`
- `timestamp`

두 요청의 `worker_type`만 다르다.

현재 WebSocket 연결에서 elder identity를 받지 않으므로 `elder_id`는 빈 문자열이다. `internal/slowtrack/service.go`에 향후 WebSocket identity 연결을 위한 TODO를 남겼다.

Reply Queue 응답 계약으로 `WorkerResponse`를 추가했다.

- job/session/elder/correlation 식별자
- worker type
- `success` 또는 `failed` 상태
- `json.RawMessage` 결과
- 선택적 오류 메시지
- timestamp

기존 `WorkerReply`는 현재 코드 및 외부 Worker와의 호환성을 위해 유지하고 deprecated 표시했다.

## 4. Reply Queue 추가 내용

다음 환경변수를 추가했다.

```env
RABBITMQ_REPLY_QUEUE=todai.reply
```

`Topology`에 `ReplyQueue`를 추가했으며 `Client.DeclareQueues()`가 emotion, STT, Reply Queue를 모두 선언한다.

Reply Queue 선언 옵션은 Worker 큐와 같다.

- durable: true
- autoDelete: false
- exclusive: false
- noWait: false
- arguments: nil

아직 Reply Queue consumer는 구현하지 않았다.

## 5. Publish 흐름 변경

변경된 흐름은 다음과 같다.

```text
발화 종료
-> 임시 job_id 및 correlation_id 생성
-> 공통 WorkerRequest 생성
-> emotion/stt 요청으로 분리
-> 3초 기본 timeout 적용
-> emotion publish 시도
-> emotion 결과와 관계없이 stt publish 시도
-> 실패 에러를 errors.Join으로 결합
```

각 publish 함수는 요청의 `worker_type`을 검증한다. 잘못된 Worker type이면 RabbitMQ publish 전에 에러를 반환한다.

Emotion과 STT publish는 둘 다 시도한다. 하나 또는 둘 다 실패하면 발생한 에러를 결합해 호출자에게 반환한다.

각 결과 로그에는 다음 식별자가 포함된다.

- `job_id`
- `correlation_id`
- Worker 종류
- 성공 또는 실패

향후 각 결과를 `job_event_history`에 기록할 위치에는 TODO를 남겼다.

## 6. 아직 구현하지 않은 것

- Reply Queue consume loop
- Aggregator
- Worker 응답 10초 timeout
- Emotion/STT 결과 병합
- `analysis_job` 생성 및 상태 변경
- `job_event_history` DB 기록
- `analysis_result`, `analysis_metric` 저장
- DB repository
- Fast Track
- publisher confirm
- retry 및 dead-letter queue
- RabbitMQ 자동 재연결
- OS signal 기반 전체 graceful shutdown

## 7. 다음 구현 제안

1. WebSocket 인증 또는 연결 파라미터에서 `elder_id`를 전달하는 계약 확정
2. 발화 publish 전에 `analysis_job`을 생성할지, publish 성공 후 생성할지 결정
3. `WorkerResponse`를 Python Worker 팀과 확정
4. Reply Queue consumer 구현
5. `job_id`와 `correlation_id` 기반 Aggregator 상태 관리
6. 10초 timeout과 부분 결과 정책 구현
7. Worker별 publish/response 이벤트를 `job_event_history`에 기록
8. publisher confirm, retry, 재연결 정책 추가

## 8. 테스트 결과

다음 단위 테스트를 추가했다.

- RabbitMQ 설정 기본값 및 환경변수 override
- 잘못된 publish timeout의 기본값 fallback
- Reply Queue가 topology 선언 목록에 포함되는지 확인
- Emotion/STT 요청이 같은 job/session/correlation 정보를 공유하는지 확인
- Worker type과 Reply Queue가 요청별로 올바르게 설정되는지 확인
- Emotion/STT worker type 검증 에러가 함께 반환되는지 확인

실행 명령:

```bash
go test ./...
```

현재 작업 환경에는 `go`와 `gofmt` 실행 파일이 없어 테스트와 자동 포맷을 실행하지 못했다. 코드 검토와 `git diff --check`를 통한 공백 오류 검증은 수행했다.
