# RabbitMQ 큐 구현 관련 현재 구현 리포트

## 1. 전체 요약

현재 구현은 RabbitMQ 최소 골격뿐 아니라, WebSocket 입력을 실제 발화 단위 메시지로 변환하는 초기 오케스트레이션까지 포함한다.

큐 구현 범위에 적합한 부분:

- RabbitMQ 연결 및 채널 관리
- emotion/stt 큐 선언
- JSON publish
- 큐 이름 환경변수 관리
- `WorkerRequest` 메시지 구조
- 애플리케이션 시작 시 publisher 초기화

큐 구현 범위를 넘어선 부분:

- 실제 PCM 기반 VAD 구현
- 침묵 1.5초를 발화 종료로 판단하는 정책
- Session 내부 chunk 누적 및 발화 단위 drain
- Handler 내부 audioData 조립
- Handler 내부 `WorkerRequest` 생성
- Handler 내부 Slow Track 비동기 실행 및 timeout 정책
- Fast Track 진입 위치 결정

현재 `internal/orchestrator` 또는 별도 Slow Track 패키지는 없다. 해당 책임은 대부분 WebSocket Handler에 들어가 있다.

## 2. 파일별 구현 현황

### `internal/queue/client.go`

- 현재 역할: RabbitMQ connection/channel 생성, 큐 선언, 자원 종료
- 큐 구현과의 관련성: 직접적인 핵심 구현
- 유지해도 되는 부분: `NewClient`, `DeclareQueues`, `Close`
- 과하게 구현되었거나 분리가 필요한 부분: 특별히 없음
- 다른 코드와의 의존 관계: `amqp091-go`, `Topology`

### `internal/queue/topology.go`

- 현재 역할: RabbitMQ URL과 큐 이름 기본값 및 큐 목록 관리
- 큐 구현과의 관련성: 직접적인 핵심 구현
- 유지해도 되는 부분: configurable queue name, 기본값
- 과하게 구현되었거나 분리가 필요한 부분: URL과 큐 이름 기본값이 config에도 중복 정의됨
- 다른 코드와의 의존 관계: `Client.DeclareQueues`, `Publisher`

### `internal/queue/publisher.go`

- 현재 역할: payload JSON 변환 및 emotion/stt 큐 publish
- 큐 구현과의 관련성: 직접적인 핵심 구현
- 유지해도 되는 부분: 개별 publish 함수, default exchange, Content-Type 설정
- 과하게 구현되었거나 분리가 필요한 부분: `PublishToWorkers`가 순차 publish 정책까지 담당
- 다른 코드와의 의존 관계: `Client`의 channel/topology, Handler의 `SlowPublisher`

### `internal/websocket/handler.go`

- 현재 역할: WebSocket 연결부터 발화 종료 후 RabbitMQ publish까지 전체 흐름 담당
- 큐 구현과의 관련성: publish 호출 연결부만 관련
- 유지해도 되는 부분: WebSocket 연결/수신, 외부 처리기로 audio를 전달하는 얇은 연결부
- 과하게 구현되었거나 분리가 필요한 부분: VAD 실행, 발화 조립, WorkerRequest 생성, timeout, goroutine, Slow Track 정책
- 다른 코드와의 의존 관계: `Session`, `vad`, `model`, `SlowPublisher`, UUID

### `internal/websocket/session.go`

- 현재 역할: 연결, chunk buffer, VAD 상태, 세션 종료 상태 보관
- 큐 구현과의 관련성: 직접적인 큐 책임은 아님
- 유지해도 되는 부분: 세션 ID, WebSocket connection, 종료 신호
- 과하게 구현되었거나 분리가 필요한 부분: `chunks`, `DrainChunks`, `ObserveVAD`, VAD 필드
- 다른 코드와의 의존 관계: `handler.go`, `internal/vad`

### `internal/vad/detector.go`

- 현재 역할: PCM 16-bit RMS 기반 음성/침묵 및 발화 종료 감지
- 큐 구현과의 관련성: 없음
- 유지해도 되는 부분: 추후 VAD 단계에서 독립적으로 검토 가능
- 과하게 구현되었거나 분리가 필요한 부분: 큐 골격 작업에서는 전체가 범위 밖
- 다른 코드와의 의존 관계: `Session`, Handler의 세션 생성

### `pkg/model/message.go`

- 현재 역할: Worker 요청·응답 및 결과 모델 정의
- 큐 구현과의 관련성: `WorkerRequest`는 직접 관련
- 유지해도 되는 부분: 최소 WorkerRequest
- 과하게 구현되었거나 분리가 필요한 부분: `WorkerReply`, 결과 구조는 아직 미사용이며 다음 단계 범위
- 다른 코드와의 의존 관계: Handler가 `WorkerRequest`를 직접 생성

### `internal/config/config.go`

- 현재 역할: 서버 포트 및 RabbitMQ 환경변수 로드
- 큐 구현과의 관련성: 직접 관련
- 유지해도 되는 부분: URL과 큐 이름 환경변수
- 과하게 구현되었거나 분리가 필요한 부분: 기본값을 `queue/topology.go`와 한 곳에서 관리하는 방안 검토
- 다른 코드와의 의존 관계: `main.go`

### `cmd/server/main.go`

- 현재 역할: 설정 로드, RabbitMQ 초기화, publisher 생성 및 Handler 주입
- 큐 구현과의 관련성: 애플리케이션 조립 지점으로 적절
- 유지해도 되는 부분: Client/Publisher 초기화와 Close
- 과하게 구현되었거나 분리가 필요한 부분: `websocket.SlowPublisher` 타입을 main이 알아야 하는 구조
- 다른 코드와의 의존 관계: config, queue, websocket

### Orchestrator / Slow Track 관련 파일

- 현재 별도 파일이나 패키지가 없다.
- Handler의 `handleUtterance`가 사실상 Slow Track 오케스트레이터 역할을 수행한다.

## 3. WebSocket Handler 책임 분석

분류 기준:

- **A:** 큐 publish 연결을 위해 유지할 책임
- **B:** 현재는 TODO/placeholder로 약화 가능한 책임
- **C:** 큐 범위를 벗어나 제거 또는 분리할 책임

| 책임 | 현재 구현 여부 | 큐 구현과의 관련성 | 분류(A/B/C) | 비고 |
|---|---|---|---|---|
| WebSocket 연결 수락 | 구현 | audio 입력 출처 | A | WebSocket 패키지 본래 책임 |
| 세션 생성/삭제 | 구현 | 간접 관련 | A | 연결 관리 범위 |
| 메시지 타입 검사 | 구현 | 간접 관련 | A | binary 입력 보호 |
| binary chunk 수신 | 구현 | publish 입력 확보 | A | 유지 가능 |
| chunk buffer 누적 | 구현 | 현재 publish 방식에만 필요 | C | 발화 조립 정책 |
| VAD 생성 및 실행 | 구현 | 직접 관련 없음 | C | Handler에서 분리 필요 |
| 발화 종료 판단 | 구현 | publish 시점 결정 | C | 미확정 정책을 고정함 |
| `DrainChunks` 호출 | 구현 | 현재 audio 조립에 사용 | C | 발화 조립 책임 |
| `bytes.Join` audioData 조립 | 구현 | payload 입력 생성 | C | 별도 처리 계층 후보 |
| Fast Track 위치 결정 | TODO 로그 구현 | 직접 관련 없음 | B/C | 현재는 로그만 존재 |
| `WorkerRequest` 생성 | 구현 | publish에 필요 | B | Handler보다 Slow Track 계층이 적합 |
| correlation ID 생성 | 구현 | publish 추적에 필요 | B | 요청 생성기로 이동 가능 |
| RabbitMQ publish 호출 | 구현 | 직접 관련 | A | 호출 자체는 필요 |
| publish goroutine 생성 | 구현 | Fast Track 비차단 목적 | B/C | 실행 정책은 Handler 밖이 적합 |
| 3초 publish timeout | 구현 | publish 안정성 관련 | B | 정책값과 소유 위치 재검토 필요 |
| publish 실패 로그 | 구현 | 직접 관련 | A | 비차단 실패 처리 적절 |

Handler가 반드시 덜어내야 할 핵심 책임은 VAD 판단, 발화 종료 정책, audioData 조립, WorkerRequest 생성 및 Slow Track 실행 정책이다.

## 4. RabbitMQ 구현 현황

- Client 생성: 구현됨. `amqp.Dial()` 사용
- Connection/channel 관리: `Client`가 단일 connection과 channel 보유
- Queue declare: Client 생성 시 자동 수행
- 선언 큐: `todai.worker.emotion`, `todai.worker.stt`
- 큐 설정: durable=true, autoDelete=false, exclusive=false
- Exchange: default exchange `""`
- Routing key: queue name
- Publish 함수: `PublishEmotion`, `PublishSTT`, `PublishToWorkers`
- PublishToWorkers: emotion 성공 후 stt를 순차 publish
- Context timeout: Handler에서 3초 timeout 생성
- JSON marshal: Publisher 내부에서 수행
- Content-Type: `application/json`
- 메시지 영속성: `amqp.Persistent`
- Close: channel 종료 후 connection 종료
- 환경변수: URL, emotion queue, stt queue 사용
- 연결 실패: 서버는 계속 실행하고 Slow Track을 비활성화
- Publish 실패: 에러 반환 후 Handler에서 로그 기록

주의점:

- emotion publish 성공 후 stt publish가 실패하면 부분 publish 상태가 발생한다.
- channel 접근은 mutex로 직렬화된다.
- 재연결, publisher confirm, retry는 아직 없다. 최소 골격 단계에서는 허용 가능한 상태다.

## 5. 메시지 구조 현황

`WorkerRequest` 필드:

- `session_id`: WebSocket 연결 수락 시 Handler가 UUID 생성
- `correlation_id`: 발화 종료 처리 시 Handler가 UUID 생성
- `audio_data`: 세션에 누적된 모든 chunk를 `bytes.Join`하여 설정
- `timestamp`: Handler에서 `time.Now().UnixMilli()` 생성
- `reply_to`: 빈 문자열로 전송되며 TODO 주석만 존재

구조 평가:

- 기존 공용 모델을 재사용한 점은 적절하다.
- `Publisher`가 `any` payload를 받아 메시지 구조 자체에는 강하게 결합되지 않는다.
- 반면 Handler는 `model.WorkerRequest`에 직접 결합되어 있다.
- `audio_data`가 PCM 16-bit, 16kHz, mono라고 코드 주석과 VAD 구현이 가정한다.
- `reply_to`와 WorkerReply 구조가 이미 정의되어 있지만 실제 Reply Queue는 구현되지 않았다.

## 6. VAD / 발화 단위 처리 현황

- VAD는 실제 동작 가능한 최소 구현이다.
- PCM 16-bit little-endian mono 데이터를 RMS로 분석한다.
- RMS 임계값은 `500`이다.
- 음성 감지 후 누적 침묵 `1500ms`를 발화 종료로 판단한다.
- chunk는 `Session.chunks`에 누적된다.
- `Handler.readLoop`가 모든 binary chunk를 누적하고 VAD를 호출한다.
- VAD가 발화 종료를 반환하면 `Handler.handleUtterance`가 `DrainChunks()`를 호출한다.
- `bytes.Join`으로 발화 단위 audioData를 생성한다.
- trailing silence chunk도 audioData에 포함된다.

평가:

- VAD 알고리즘은 별도 패키지에 있지만, VAD 실행 및 발화 종료 정책은 Handler가 직접 담당한다.
- 원래 목표에서 VAD 및 발화 조립 정책이 미확정이므로 현재 단계에서는 분리하거나 placeholder로 약화하는 것이 적절하다.
- 큐 publish를 실제 WebSocket 입력과 연결하려면 어떤 형태로든 완성된 audioData 전달 시점은 필요하지만, 그 시점을 Handler가 VAD로 결정할 필요는 없다.

## 7. 원래 목표 대비 과하게 구현된 부분

### 유지 가능

- `internal/queue` 전체 골격
- RabbitMQ 환경변수
- `WorkerRequest`
- main의 RabbitMQ 초기화 및 Close
- publish 실패 시 WebSocket 흐름을 막지 않는 처리
- Handler가 추상화된 publisher 또는 처리기를 호출하는 연결 지점

### 약화 권장

- Handler 내부 `WorkerRequest` 생성
- Handler 내부 3초 timeout 정책
- Handler 내부 goroutine 실행 정책
- Fast Track placeholder 로그
- `ReplyTo` TODO
- `WorkerReply`, 결과 모델의 현재 노출

### 제거 또는 분리 검토

- Handler의 `vad.NewDetector()` 호출
- Session의 VAD 필드
- Handler의 `ObserveVAD()` 호출
- Session의 chunk 누적 및 `DrainChunks`
- Handler의 발화 종료 판단
- Handler의 `bytes.Join`
- Handler가 Slow Track 정책을 직접 실행하는 구조

Reply Queue, Aggregator, ADK, DB 선행 구현은 현재 존재하지 않는다.

## 8. 제거/약화 시 영향 받는 파일

VAD와 발화 조립을 Handler에서 제거하면 다음 영향이 있다.

### `internal/websocket/handler.go`

- `bytes`, `time`, `vad`, `model`, UUID 일부 import 정리 필요
- `handleUtterance` 제거 또는 외부 처리기 호출 형태로 변경 필요
- `ObserveVAD`, `DrainChunks` 호출 제거 필요

### `internal/websocket/session.go`

- `vad` 필드와 import 제거 가능
- `chunks`, `appendChunk`, `DrainChunks`, `TotalBuffered`가 미사용될 가능성
- `newSession` 인자 변경 필요

### `internal/vad/detector.go`

- 참조가 없어지면 독립 보관 또는 삭제 후보

### `cmd/server/main.go`

- Handler 생성자 계약이 변경될 경우 영향

### `pkg/model/message.go`

- Handler에서 제거해도 queue/Slow Track 계층에서 재사용 가능

### `internal/queue`

- VAD/조립 제거에 직접 영향 없음

지금 당장 VAD 호출만 삭제하면 publish 진입점 자체가 사라진다. 대체 진입점 없이 제거하면 큐는 선언되지만 WebSocket audio가 publish되지 않는다.

## 9. 추천 정리 방향

### 1안: 최소 수정안

- 내용: 현재 구조를 유지하되 Handler의 VAD/발화 처리 부분에 임시 구현임을 명확히 표시
- 장점: 현재 end-to-end publish 확인 가능
- 단점: Handler 책임 과다가 계속됨
- 수정 대상 파일: Handler, Session 주석 중심
- 유지되는 기능: VAD 기반 실제 publish 전체
- 제거/약화되는 기능: 정책이 임시라는 점만 명시
- 추천 여부: 단기 데모 목적일 때만 추천

### 2안: 책임 분리안

- 내용: `internal/orchestrator` 또는 `internal/slowtrack`을 만들고 Handler는 audio 이벤트만 전달
- 장점: 큐, WebSocket, VAD, 요청 생성 책임이 명확해짐
- 단점: 현재 단계 기준 파일과 인터페이스가 늘어남
- 수정 대상 파일: Handler, Session, main, 신규 orchestrator/slowtrack
- 유지되는 기능: 현재 실제 VAD 기반 publish
- 제거/약화되는 기능: 없음. 위치만 분리
- 추천 여부: 장기 구조로 가장 추천

### 3안: 큐 구현만 남기고 나머지 약화

- 내용: VAD와 발화 조립 연결을 제거하고, 완성된 `audioData`를 받는 publish 진입점만 제공
- 장점: 원래 큐 담당 범위와 가장 정확히 일치
- 단점: WebSocket 입력만으로는 자동 publish되지 않음
- 수정 대상 파일: Handler, Session, main, VAD 참조
- 유지되는 기능: RabbitMQ 연결·선언·payload publish
- 제거/약화되는 기능: 실제 발화 종료 및 자동 publish 흐름
- 추천 여부: 현재 목표를 엄격히 지키려면 추천

가장 현실적인 방향은 **2안의 얇은 형태**다. Handler는 완성된 audio 또는 audio 이벤트를 외부 처리기로 넘기고, 현재 VAD 구현은 임시 어댑터로 격리하는 방식이다.

## 10. 최종 결론

- 현재 구현 중 큐 담당자가 유지해도 되는 부분은 `internal/queue`, RabbitMQ config, `WorkerRequest`, main의 Client/Publisher 초기화와 종료 처리다.
- 현재 구현 중 큐 담당 범위를 넘어선 부분은 VAD, 침묵 기준, chunk 누적, 발화 종료 판단, audioData 조립, Fast/Slow Track 실행 정책이다.
- WebSocket Handler에서 반드시 덜어내야 할 책임은 VAD 정책, 발화 단위 조립, WorkerRequest 생성, Slow Track goroutine/timeout 정책이다.
- 지금 당장 삭제하면 위험한 부분은 `ObserveVAD -> handleUtterance -> PublishToWorkers` 흐름이다. 대체 진입점 없이 삭제하면 실제 publish가 완전히 사라진다.
- 다음 Codex 작업으로 적절한 리팩터링 프롬프트는 아래와 같다.

```text
현재 RabbitMQ queue 패키지와 publish 기능은 유지하고,
WebSocket Handler에서 VAD 판단, chunk 조립, WorkerRequest 생성,
Slow Track goroutine 및 timeout 책임을 분리해주세요.

Handler는 WebSocket 연결/세션/바이너리 입력 수신까지만 담당하고,
완성된 audioData를 전달받아 WorkerRequest를 생성하고 publish하는 책임은
별도 slowtrack 또는 orchestrator 컴포넌트로 이동해주세요.

현재 동작하는 VAD 기반 publish 흐름은 깨지지 않게 유지하되,
VAD 정책을 Handler와 queue 패키지에서 독립시켜주세요.
Reply Queue, Aggregator, ADK, DB는 구현하지 마세요.
```
