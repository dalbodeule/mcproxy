# mcproxy Admin Panel

mcproxy와 분리 배포하는 SvelteKit 3+ / Svelte 5 / TailwindCSS 운영 패널입니다.
패널 서버가 mcproxy API를 호출하므로 `MCPROXY_ADMIN_TOKEN`은 브라우저에 노출되지
않습니다.

## 기능

- Gate, backend, Redis, cache, audit, block 상태 dashboard
- 서버 생성/삭제/활성화/비활성화 및 hostname routing
- 서버별 forwarding mode와 BungeeGuard 토큰 관리
- 정책, GeoIP, allow/deny, rule 관리
- 감사 로그 및 운영자 actor 기록
- 초대 기반 회원제, RBAC, 서버 scope, 세션, 비밀번호 변경
- TOTP MFA, 한/영/일 locale, 반응형·고대비 UI
- 백업/복구 및 membership migration 검사

## 역할과 권한

| 역할 | 권한 |
| --- | --- |
| Owner | 모든 관리 기능과 회원/시크릿 관리 |
| Admin | 서버·정책·룰·토큰·회원 관리 |
| Operator | 서버 운영 상태와 활성화/비활성화 |
| Auditor | 통계와 감사 로그 조회 |
| Viewer | 읽기 전용 dashboard |

역할 검사는 페이지 action에만 의존하지 않고 server-side API proxy, path,
query, JSON body에도 적용됩니다. 서버 scope가 있는 회원은 허용된 서버만
조회·수정할 수 있습니다.

## 로컬 실행

```sh
cp .env.example .env
npm install
npm run dev
```

주요 server-only 환경변수:

- `MCPROXY_API_URL`: 내부 mcproxy API 주소
- `MCPROXY_ADMIN_TOKEN`: mcproxy 관리자 토큰
- `MCPROXY_ADMIN_ACTOR_SECRET`: mcproxy와 동일하게 설정하는 선택적 actor 서명 secret
- `ORIGIN`: 패널의 정확한 public origin
- `ADMIN_PANEL_USERNAME`, `ADMIN_PANEL_PASSWORD`
- `ADMIN_PANEL_SESSION_SECRET`: 32자 이상의 랜덤 session secret
- `ADMIN_PANEL_DB_PATH`: 회원/초대 SQLite 경로

모든 secret은 `*_FILE` 형식의 mounted secret도 지원합니다. 직접 지정한 환경
변수가 file 값보다 우선합니다.

## 운영 배포

패널은 TLS termination과 identity-aware reverse proxy 또는 VPN 뒤에 두십시오.
컨테이너 기본 포트는 `3000`이며 `/health`는 liveness, `/ready`는 session secret,
회원 테이블, migration version, SQLite 저장소를 검사합니다.

배포 전:

```sh
MCPROXY_API_URL=http://mcproxy:8080 \
ORIGIN=https://panel.example.com \
MCPROXY_ADMIN_TOKEN_FILE=/run/secrets/mcproxy-admin-token \
ADMIN_PANEL_SESSION_SECRET_FILE=/run/secrets/panel-session-secret \
PANEL_PREFLIGHT_REQUIRE_HTTPS=true \
./deploy/panel-preflight.sh
```

Reverse proxy가 `X-Forwarded-Proto`를 덮어써서 전달할 때만 다음을 설정합니다.

```env
PROTOCOL_HEADER=x-forwarded-proto
```

클라이언트가 직접 전달한 forwarding header를 신뢰한 채 패널을 공개하지 마십시오.

## 데이터와 migration

회원·초대·세션·TOTP 정보는 `ADMIN_PANEL_DB_PATH`의 SQLite에 저장됩니다.
현재는 SQLite가 단일 replica 운영만 지원합니다. 다중 replica가 필요하면 공유
membership DB adapter를 도입하기 전까지 다중 replica로 확장하지 마십시오.

패널 image 업그레이드나 회원/비밀번호 변경 전 DB를 백업합니다. 패널은 시작 시
additive migration을 적용하며, migration 실패·잠금·손상·쓰기 불가 DB는 fatal로
처리합니다. `ADMIN_PANEL_SESSION_SECRET`을 교체하면 기존 세션이 무효화되고
암호화된 TOTP secret을 다시 등록해야 합니다.

```sh
make panel-check
make panel-smoke
make panel-membership-smoke
make panel-security-smoke
make panel-backup-smoke
make panel-rehearsal
```

## 보안 기본값

- mcproxy token과 actor secret은 server-side only
- session cookie는 signed/httpOnly이고 logout·비밀번호 변경 시 폐기
- 상태 변경 JSON 요청은 same-origin `Origin` 검사 필요
- API proxy는 `stats`, `audits`, `servers`, `policy`, `rules` 경로만 허용
- 회원과 초대 응답은 `Cache-Control: no-store`
- 로그인 실패는 IP 및 username 기준으로 제한
- BungeeGuard token은 Owner/Admin/Operator에게만 노출
- 회원 scope와 감사 로그 server filter는 서버 측에서 재검증

자세한 회원제 설계는 [docs/admin-panel-membership.md](../docs/admin-panel-membership.md),
전체 보안 경계는 [SECURITY.md](../SECURITY.md)를 참고하십시오.
