# mcproxy 로드맵

현재 구현과 이후 목표를 구분하기 위한 실행 기준입니다. 기능을 추가할 때에는
보안 경계, migration, 운영 검증, 문서 갱신을 함께 완료합니다.

## 완료된 범위

- Gate `v0.74.41` 기반 Minecraft 26.3 / protocol 777 대응
- `minecraft:hello` 디코딩 및 `encoding/json/v2` audit panic 수정
- 서버별 forwarding과 BungeeGuard token 생성/교체
- SQLite/PostgreSQL migration, backup/restore, audit pipeline
- Redis 분산 카운터, fail-closed readiness, metrics/alerts
- IP/nickname/GeoIP/ASN 정책과 서버별 scope
- SvelteKit 3+ / TailwindCSS 관리 패널
- 한/영/일 다국어, RBAC, 초대 회원제, MFA, 감사 actor
- Docker/Compose/Kubernetes, preflight, smoke test, CI security gate

## 다음 단계: 운영 안정화

- 실제 백엔드별 BungeeGuard/Velocity/legacy 연결 매트릭스 검증
- 토큰 rotation 후 plugin reload 및 rollback runbook 확정
- PostgreSQL/Redis TLS, SecretStore, egress CIDR을 실제 provider 값으로 교체
- Prometheus alert threshold를 정상 peak와 장애 상황에 맞게 튜닝
- 백업 암호화, 보존 기간, 복구 RTO/RPO 확정
- production image 서명, SBOM 보관, release artifact 검증
- 26.3 login/status/switch, backend disconnect, Redis outage staging 반복 테스트

완료 조건: staging에서 migration, rollback, restore, token rotation, Redis 장애
복구를 문서대로 수행하고 운영자가 결과를 재현할 수 있어야 합니다.

## 다음 단계: 관리 패널 완성

- 운영자 피드백 기반 dashboard 카드와 경보 우선순위 조정
- 서버 목록/정책/감사 로그의 검색, 필터, pagination 개선
- 토큰 교체 전후 diff와 backend 적용 확인 상태 제공
- 위험 작업 확인, 재시도, 실패 원인 안내 강화
- 운영 이벤트 timeline과 incident 메모 기능 검토
- 번역 누락 검사와 한/영/일 용어집 유지
- 키보드 탐색, screen reader, 고대비, 모바일 접근성 점검

완료 조건: 운영자가 API를 직접 호출하지 않고 일반적인 서버·정책·토큰 작업을
수행할 수 있고, 실패 시 다음 조치를 화면에서 이해할 수 있어야 합니다.

## 다음 단계: 회원제와 권한

현재 권장 모델은 공개 회원가입이 아닌 초대 기반 self-hosted 운영자 회원제입니다.

- Owner/Admin/Operator/Auditor/Viewer 권한 유지
- 서버 scope를 모든 API 조회·변경에 계속 강제
- WebAuthn은 TOTP 사용자의 실제 수요 확인 뒤 추가
- API token은 평문 대신 hash 저장하는 별도 설계 검토
- 조직/tenant 분리는 SaaS 전환이 확정된 경우에만 시작

SaaS를 추진할 때는 먼저 `organizations`, `memberships`, `api_tokens`,
`audit_events`의 tenant 격리를 구현하고 모든 query/mutation에 조직 scope를
강제해야 합니다. UI에서만 숨기는 방식은 허용하지 않습니다.

## 중기 목표: 확장성

- 패널 membership 저장소의 PostgreSQL adapter 및 다중 replica 지원
- 외부 OIDC 로그인, 이후 기업 수요가 있을 때 SAML/SCIM
- 여러 mcproxy 인스턴스를 한 패널에서 관리하는 연결 모델
- 중앙 감사 로그 보존과 export
- 정책 변경 versioning, preview, rollback
- 필요 시 worker/queue 기반 audit 및 알림 확장

## 장기 목표: 서비스화 검토

- 조직별 서버/정책/감사 로그 격리
- 플랜별 서버 수, 사용자 수, 보존 기간 정책
- billing과 Minecraft 접속 트래픽을 분리한 과금 모델
- 고객별 SSO, audit export, SLA/지원 체계
- 관리 plane과 data plane의 장애 도메인 분리

공개 회원가입, 결제, Minecraft 플레이어 계정 인증은 현재 범위가 아닙니다.
