# Argo Watcher Web UI

The Web UI: Vite, React 19, React-admin 5, Material UI 9, TypeScript, oxlint, Vitest/jsdom, Playwright. Authentication goes through any OIDC provider via `oidc-client-ts`, with a fully anonymous mode when the backend reports `oidc.enabled = false`.

The build output in `dist/` is what the Go server serves (`STATIC_FILES_PATH`), and what the Docker images copy in.

## Development

```bash
nix develop        # or direnv allow — provides Go + Node toolchains and the Playwright browsers
cd web
npm install
npm run dev        # http://localhost:5173, proxying /api and /ws to VITE_API_PROXY_TARGET
```

The Go API must be running (`task bootstrap` from the repo root, or `go run ./cmd/argo-watcher`) unless you repoint the proxy. Start it with `DEV_ENVIRONMENT=true`: the dev server is a different origin from the API, and the server allows no cross-origin request without it.

| Command | Description |
|---|---|
| `npm run dev` | Dev server with proxy and HMR |
| `npm run build` | Production build into `dist/` |
| `npm run preview` | Serve `dist/` with production routing |
| `npm run lint` | oxlint (see `.oxlintrc.json`) |
| `npm run test` | Vitest once, with text + LCOV coverage |
| `npm run test:watch` / `test:ui` | Interactive Vitest |
| `npm run test:e2e` | Playwright — expects `dist/` built and Keycloak up; `task test-web-e2e` does both |
| `npm run test:e2e:install` | Install the Chromium build Playwright drives |
| `npm run typecheck:e2e` | Type-check the node-side project (`playwright.config.ts`, `vite.config.ts`, `e2e/`). Playwright does not type-check specs, so this is its own CI gate; `src/` is not covered. |
| `npm run scan:js` | retire.js scan for known-vulnerable JavaScript libraries, including ones a bundled dependency vendors in without declaring (`swagger-ui-dist` inlines DOMPurify this way). `task scan-web` runs the same check. Needs network for the advisory repository. |

## Configuration

Everything runtime comes from the backend's `/api/v1/config` — issuer URL, client id, privileged groups — which the frontend caches and only acts on when `oidc.enabled` is true. Only three build-time variables exist:

| Variable | Default | Purpose |
|---|---|---|
| `VITE_API_PROXY_TARGET` | `http://localhost:8080` | Dev-only proxy target for `/api` and `/ws` |
| `VITE_API_BASE_URL` | `''` (same origin) | Prefix for every REST call — of use only against a server that admits the calling origin, which outside `DEV_ENVIRONMENT=true` means the same origin |
| `VITE_WS_BASE_URL` | `''` (from `window.location`) | WebSocket origin, when tunnelling through another host |

## Layout

| Path | Purpose |
|---|---|
| `src/main.tsx` | Mounts `AppSplash` immediately, awaits `bootstrapAuth()` to consume any OIDC callback, then mounts React-admin with the shared providers. |
| `src/App.tsx` | The React-admin shell: the `tasks` resource plus custom routes for history and task detail. |
| `src/auth/` | `authProvider.ts` (OIDC via `oidc-client-ts`, configured from `/api/v1/config`), `authFailure.ts` (the `AuthFailure` the splash screen renders), `tokenStore.ts` (token in memory only). |
| `src/data/` | `httpClient.ts`, the React-admin `dataProvider`, and `wsStatusService.ts` — the shared REST-bootstrap + `/ws` protocol. |
| `src/features/` | Feature modules: `tasks/`, `deployLock/`, `argocdStatus/`. Each owns its components, hooks and services. |
| `src/layout/` | Layout primitives (top bar, nav, notifications) and `AppSplash.tsx`, the pre-mount loading screen. |
| `src/shared/` | Cross-cutting hooks, providers (theme mode, timezone) and utilities. |
| `src/theme/` | MUI theme factory and `ThemeModeProvider` (light/dark, persisted in `localStorage`). |
| `e2e/` | Playwright specs, split into the `no-auth` and `auth` projects, plus `helpers.ts`. |
| `dist/` | Build output served by the Go server. |

## Design notes worth knowing before changing things

- **`bootstrapAuth()` must be awaited in `main.tsx`, before React mounts.** It consumes the `?code=…&state=…` callback before the router's index redirect can strip it. Moving callback handling back into the React tree reintroduces a redirect loop.
- **It resolves to `AuthFailure | null`.** Non-null means the sign-in cannot complete, and `main.tsx` then leaves React-admin unmounted and shows the reason on the splash — mounting it would re-run `checkAuth` and head straight back to the provider that just failed. A `/api/v1/config` request that merely *fails* resolves `null` on purpose, so a restarting backend still renders the app.
- **`AppSplash` renders outside `AppProviders` and issues no requests.** Those providers start polling on mount, before any token exists. Given an `error` prop it becomes the failure surface, with **Try again** as the only retry.
- **`checkAuth` never rejects for an unauthenticated user** — it redirects. Rejecting triggers a logout loop.
- **Permissions come from the userinfo endpoint**, the same source the backend enforces on, so the UI cannot show a control the API will reject (except while userinfo is unreachable).
- **`wsStatusService` owns the real-time protocol**: REST bootstrap, then `/ws` transitions, one socket per signal while listeners exist, reconnect-and-reconcile on drop, and ordering guards so a slow REST response cannot overwrite a fresher push. `deployLockService` adds the lock/release operations; `argocdStatusService` extends it read-only.

## Testing

Vitest runs in jsdom with `vitest.setup.ts`; tests live beside the code as `*.test.ts(x)`. Keep the auth provider, data provider, the shared WS protocol, and the utilities covered — coverage goes to Codecov.

Playwright covers only what jsdom structurally cannot: real cross-document navigation, a real history stack, a real WebSocket.

| Project | Covers |
|---|---|
| `no-auth` | The server's SPA fallback for a deep-linked task URL; Back from a directly-opened task page; and the auth-less mode, which must render with no sign-in and no privileged controls. |
| `auth` | The top-level OIDC redirect and the stripping of its callback query; returning to the linked task via `url_state`; session recovery through the SSO cookie after a reload; the deploy-lock banner arriving over the live socket and being toggled from the drawer; rollback as a privileged user; and privilege gating for a privileged versus a regular user. |

Two constraints when adding specs:

- `@playwright/test` is pinned to the `playwright-driver` version in `flake.nix`, which supplies the browsers. Bump them together.
- Both servers hold in-memory state that every spec seeds into, so the suite is single-worker. Give each seeded task a distinct author, and never wait on the task table to decide the app has loaded — an empty list has no column headers. Use `expectAppLoaded`.

The Keycloak client in `test/keycloak/argo-watcher-e2e-realm.json` has the authorization-code flow enabled with the exact redirect URI `http://localhost:8101/` and requires PKCE (`S256`), so a regression that stopped sending a code challenge fails every auth spec. The Go integration tests use the same client through the direct access grant, so both flows stay enabled.

## Troubleshooting

- **Dev server cannot reach the API** — check `VITE_API_PROXY_TARGET`. Pointing `VITE_API_BASE_URL` at another origin needs that server to admit this one, which only `DEV_ENVIRONMENT=true` does.
- **Endless OIDC redirects** — the provider must list this app's origin (including any base path) as a valid redirect URI. If the loop only appears *after* a successful login, the callback query was stripped before `oidc-client-ts` read it; see the `bootstrapAuth()` note above.
- **WebSocket errors** — confirm `/ws` is exposed and upgraded end to end; set `VITE_WS_BASE_URL` when a TLS terminator gets in the way.
- **Timezones look wrong** — the preference lives under `argo-watcher:timezone` and is toggled from the user menu.
