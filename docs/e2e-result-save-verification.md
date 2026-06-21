# 2차-C 통합 검증 결과

## 1. 실행 환경

- Spring: 미실행으로 판단. `curl http://localhost:8080/health` 및 `/api/internal/analysis-jobs` 접속 실패.
- Middleware: 미실행. 로컬 8080 포트 listener 없음.
- RabbitMQ: 미확인/미실행으로 판단. `lsof`에서 5672/15672 listener 없음. `nc localhost 5672`는 sandbox 권한으로 차단됨.
- ADK: 미실행으로 판단. 확인 가능한 ADK health endpoint 정보가 없고, 일반적인 로컬 API 포트 listener도 확인되지 않음.
- Worker: 미실행/미확인. `ps`는 sandbox 권한으로 차단됐고, Docker daemon도 실행 중이 아님.

추가 환경 확인:

- `.env*` 파일 없음.
- Docker CLI는 존재하지만 Docker daemon에 연결되지 않음.
- DB 접속 정보 또는 로컬 DB 파일 없음.
- `go`, `gofmt` 실행 파일 없음.

## 2. 테스트 결과

- Go `go test ./...`: 실행 실패. 원인: `zsh:1: command not found: go`.
- `gofmt`: 실행 실패. 원인: `zsh:1: command not found: gofmt`.
- Spring API 연결: 실패. `localhost:8080` 연결 불가.
- Middleware E2E: 미수행. Middleware/Spring/RabbitMQ/ADK/Worker 실행 환경이 없음.
- 정적 검증: 수행. `git diff --check` 통과.

## 3. 성공 케이스

실제 E2E 성공 케이스는 실행하지 못했다.

- job_id: 미확인
- correlation_id: 미확인
- analysis_job.status: 미확인
- analysis_result.analysis_status: 미확인
- analysis_result.adk_status: 미확인
- saved_metric_count: 미확인

정적 코드 기준 기대 동작:

- ADK 성공 시 `analysis_status=SUCCESS`, `adk_status=SUCCESS`.
- metrics는 `social_isolation`, `health_anxiety`, `daily_vitality`, `emotion_variance`, `cognitive_load` 5개를 전송.
- Spring result 저장 경로는 `/api/internal/analysis-jobs/{job_id}/result`.

## 4. ADK 실패 케이스

실제 ADK 실패 E2E는 실행하지 못했다.

- job_id: 미확인
- analysis_job.status: 미확인
- analysis_result.analysis_status: 미확인
- analysis_result.adk_status: 미확인
- adk_error_reason: 미확인

정적 코드 기준 기대 동작:

- ADK 호출 조건은 emotion result 존재, STT text 존재, ADK enabled.
- ADK `/analyze` 호출 실패 시 `adk_status=FAILED`.
- `analysis_status=PARTIAL`.
- `metrics=null`.
- `adk_error_reason`에 ADK error message 저장.
- result 저장 실패는 로그만 남기고 handler 흐름을 중단하지 않음.

## 5. ADK skipped 케이스

실제 ADK skipped E2E는 실행하지 못했다.

- job_id: 미확인
- analysis_job.status: 미확인
- analysis_result.analysis_status: 미확인
- analysis_result.adk_status: 미확인
- error_reason: 미확인

정적 코드 기준 기대 동작:

- emotion result 없음, STT text 없음, ADK disabled 중 하나면 `adk_status=SKIPPED`.
- `completed` + `SKIPPED`는 `analysis_status=PARTIAL`.
- `partial_failed`, `partial_timeout` + `SKIPPED`는 `analysis_status=PARTIAL`.
- `failed`, `timeout` + `SKIPPED`는 `analysis_status=FAILED`.
- `partial_timeout` error_reason은 `one or more workers timed out`.
- `failed` error_reason은 `all workers failed`.
- `timeout` error_reason은 `all workers timed out`.

## 6. DB 확인

DB 접속 정보와 실행 중인 DB가 없어 SQL 확인은 수행하지 못했다.

수행하지 못한 쿼리:

```sql
SELECT *
FROM analysis_job
ORDER BY id DESC
LIMIT 5;
```

```sql
SELECT *
FROM analysis_result
ORDER BY id DESC
LIMIT 5;
```

```sql
SELECT *
FROM analysis_metric
ORDER BY id DESC
LIMIT 20;
```

중복 확인도 수행하지 못했다.

```sql
SELECT analysis_job_id, COUNT(*)
FROM analysis_result
GROUP BY analysis_job_id
HAVING COUNT(*) > 1;
```

```sql
SELECT analysis_result_id, metric_type, COUNT(*)
FROM analysis_metric
GROUP BY analysis_result_id, metric_type
HAVING COUNT(*) > 1;
```

정적 코드 기준 확인:

- analysis_result 중복 여부: DB 미접속으로 미확인.
- analysis_metric 중복 여부: DB 미접속으로 미확인.
- emotion raw score 저장 여부: Middleware result 저장 DTO에는 emotion raw score 필드가 없음.

## 7. 발견한 문제

### 문제 1: E2E 실행 환경 없음

- 문제: Spring, Middleware, RabbitMQ, ADK, Worker가 실행 중인 상태를 확인할 수 없었고, Docker daemon도 실행 중이 아니었다.
- 원인: 로컬 실행 환경 미구성 또는 현재 sandbox 권한 제한.
- 수정 필요 여부: 코드 수정 대상은 아님. 통합 검증 환경 기동 필요.

### 문제 2: Go toolchain 없음

- 문제: `go test ./...`와 `gofmt` 실행 실패.
- 원인: 현재 shell 환경에 `go`, `gofmt` 명령이 없음.
- 수정 필요 여부: 코드 수정 대상은 아님. Go toolchain 설치 또는 PATH 설정 필요.

### 문제 3: 실제 DB 검증 불가

- 문제: `analysis_result`, `analysis_metric` 저장 여부를 SQL로 확인하지 못함.
- 원인: DB 접속 정보, 실행 중인 DB, 로컬 DB 파일을 확인하지 못함.
- 수정 필요 여부: 코드 수정 대상은 아님. Spring DB 접속 환경 필요.

## 8. 정적 검증 결과

### API path

Middleware backend client는 `/api/internal/...` prefix를 사용한다.

확인된 경로:

- `POST /api/internal/analysis-jobs`
- `POST /api/internal/analysis-jobs/{job_id}/events`
- `PATCH /api/internal/analysis-jobs/{job_id}/status`
- `POST /api/internal/analysis-jobs/{job_id}/result`

`internal` 및 `cmd` 코드에서 `"/internal/analysis-jobs"` 호출 문자열은 발견되지 않았다.

### SaveAnalysisResultRequest DTO

확인 결과:

- `stt_text` JSON tag 정상.
- `metrics` JSON tag 정상.
- `SaveAnalysisResultRequest`의 result 저장 필드에는 `omitempty` 없음.
- `STTText`, `Metrics`, `SummaryText`, `OverallScore`, `ErrorReason`, `ADKErrorReason`는 pointer type으로 JSON `null` 전송 가능.
- emotion raw score 필드 없음.

DTO에 없는 필드:

- `emotion`
- `emotion_sadness`
- `emotion_anxiety`
- `emotion_neutral`
- `emotion_joy`

### Result save failure 처리

`cmd/server/main.go`에서 `SaveAnalysisResult()` 실패 시 로그만 남긴다.

```text
backend result save failed
```

handler는 error를 반환하지 않고 `nil`을 반환하므로 Middleware 전체 흐름을 죽이지 않는다.

## 9. 다음 작업

- Spring, RabbitMQ, ADK, Worker, Middleware를 같은 환경에서 기동.
- Go toolchain 설치 또는 PATH 설정 후 `gofmt`와 `go test ./...` 실행.
- 실제 WebSocket audio 또는 테스트 입력으로 Slow Track 실행.
- Spring에서 `analysis_job` 생성 확인.
- RabbitMQ worker 응답 수신 및 Aggregator final status 확인.
- Spring status update 확인.
- ADK success/failed/skipped 세 케이스 실행.
- Spring result save 호출 확인.
- DB에서 `analysis_result`, `analysis_metric` 저장 및 중복 여부 확인.
- 사회복지사 뷰에 `analysis_metric` 반영 여부 검토.
- 필요 시 조회 API 보강.
