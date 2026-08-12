# Argo Watcher React-admin Frontend

This workspace contains the Vite/React-admin rewrite of the Argo Watcher UI. The goals of the new shell are:

- reduce custom dashboard code in favour of React-admin resources and generators
- share layouts, deploy-lock state, and theming logic with the Go backend
- keep the developer workflow fast (Vite + HMR) while still producing static assets that the Go server can embed

React 19, React-admin 5, Material UI 9, Emotion, TypeScript, oxlint, and Vitest/JSDOM are the primary dependencies. Authentication uses any OIDC provider (Keycloak, Authentik, …) via `oidc-client-ts`, with a fully anonymous mode when the backend returns `oidc.enabled = false`.

## Directory Layout

| Path | Purpose |
| ---- | ------- |
| `src/main.tsx` | React entry point that mounts `AppSplash` immediately, runs `bootstrapAuth()` to consume any OIDC authorization-code callback before mounting React-admin, then wires React Router, shared providers, and the `App` component. |
| `src/App.tsx` | Placeholder React-admin shell; extend this file with `<Admin>` resources as migration phases land. |
| `src/auth/` | Authentication helpers. `authProvider.ts` drives the OIDC provider (via `oidc-client-ts`) using config from `/api/v1/config`; `authFailure.ts` turns a provider error, an unusable callback, a redirect that never started, or an incomplete server config into the `AuthFailure` the splash screen shows; `tokenStore.ts` holds the bearer token in memory. |
| `src/data/` | HTTP layer, React-admin `dataProvider` implementation targeting `/api/v1/tasks` and related endpoints, and `wsStatusService.ts`, the shared base for signals mirrored over REST + `/ws`. |
| `src/features/` | Feature modules (currently `tasks/` and `deployLock/`). Each folder owns its components, hooks, and service logic. |
| `src/layout/` | Reusable React-admin layout primitives (notifications, top bar, nav, etc.), plus `AppSplash.tsx`, the pre-mount loading screen. |
| `src/shared/` | Cross-cutting hooks, context providers (timezone), and utilities (time formatting, OIDC toggle). |
| `src/theme/` | Material UI theme factory plus `ThemeModeProvider` for light/dark persistence. |
| `src/test/` | Test doubles shared across suites (currently the WebSocket stand-in). |
| `e2e/` | Playwright browser specs, split into the `no-auth` and `auth` projects, plus `helpers.ts` (task seeding, Keycloak sign-in, token minting). |
| `assets/` + `public/` | Static assets (logos, favicons) delivered verbatim by Vite. |
| `dist/` | Build output consumed by the Go server (`STATIC_FILES_PATH`) or Docker images. Generated via `npm run build`. |

## Environment & Runtime Configuration

All configuration is driven through the backend plus a small set of Vite runtime variables:

| Variable | Default | Usage |
| -------- | ------- | ----- |
| `VITE_API_PROXY_TARGET` | `http://localhost:8080` | Dev-only proxy target for `/api` + `/ws` when running `npm run dev`. |
| `VITE_API_BASE_URL` | `''` (same origin) | Prepended to every REST call executed by `httpClient`. Set this when serving the SPA from a CDN or different domain. |
| `VITE_WS_BASE_URL` | `''` (derived from `window.location`) | Optional WebSocket origin for the deploy-lock service. Provide when tunneling through another host. |

OIDC settings (issuer URL, client_id, privileged groups) come from `/api/v1/config` under the `oidc` key. The frontend caches the config and only attempts SSO flows when `oidc.enabled` is true.

## Development Workflow

1. **Enter the dev shell (recommended)**
   ```bash
   nix develop        # or direnv allow (uses flake.nix to expose Go + Node toolchains)
   ```
   The shell exports `nodejs@24`, pnpm/corepack helpers, Vite shim, Go toolchain, and pre-commit hooks.

2. **Install dependencies** (run once per clone or after package updates)
   ```bash
   cd web
   npm install
   ```

3. **Start the Vite dev server**
   ```bash
   npm run dev
   ```
   - Serves `http://localhost:5173` with hot module reloading.
   - Proxies `/api` to `VITE_API_PROXY_TARGET` and upgrades `/ws` for deploy-lock streaming.
   - Ensure the Go API (or `docker-compose up`) is running locally on port 8080 unless you updated the proxy target.

4. **Preview the production build locally**
   ```bash
   npm run build
   npm run preview   # serves dist/ with the same routing as production
   ```

## npm Scripts

| Command | Description |
| ------- | ----------- |
| `npm run dev` | Vite dev server with proxy + HMR. |
| `npm run build` | Production build with sourcemaps -> `web/dist`. |
| `npm run preview` | Serves `dist/` to validate routing before shipping. |
| `npm run lint` | oxlint (React, hooks, and TypeScript correctness rules; see `.oxlintrc.json`). |
| `npm run test` | Vitest in CI mode (`--run`). Generates text + LCOV coverage. |
| `npm run test:watch` | Interactive Vitest watch mode. |
| `npm run test:ui` | Vitest UI (requires a browser) for debugging complex suites. |
| `npm run test:e2e` | Playwright browser suite. Expects `web/dist` built and Keycloak up — `task test-web-e2e` from the repo root does both. |
| `npm run typecheck:e2e` | Type-checks the node-side project (`playwright.config.ts`, `vite.config.ts`, `e2e/`). Playwright does not type-check specs, so this is a separate CI gate. `src/` is not covered. |

## Data, Auth, and Real-time Architecture

- **HTTP client (`src/data/httpClient.ts`)** – wraps `fetch`, injects `Authorization`/`Oidc-Authorization` headers from `tokenStore`, normalises errors into `HttpError`, and provides helpers such as `buildQueryString`.
- **React-admin `dataProvider` (`src/data/dataProvider.ts`)** – currently implements the `tasks` resource (list/detail/create) and infers pagination totals when the backend omits them. Extend this file when exposing additional Argo watcher endpoints.
- **Auth provider (`src/auth/authProvider.ts`)** – fetches `/api/v1/config` and, when OIDC is enabled, drives an `oidc-client-ts` `UserManager` (Authorization Code + PKCE). `bootstrapAuth()` (called from `main.tsx` before React mounts) consumes the `?code=…&state=…` callback before the router's index redirect can strip it. It uses a top-level login redirect, relies on `automaticSilentRenew` for token renewal, keeps tokens in memory only, and reads group membership from the userinfo endpoint (the same source the backend enforces on) to expose permissions to React-admin. `checkAuth` never rejects for an unauthenticated user (which would trigger a logout loop) — it redirects instead. When OIDC is disabled, `bootstrapAuth()` is a no-op and the app renders immediately. `bootstrapAuth()` accepts an optional `onOidcEnabled` callback, invoked only once the config reports OIDC enabled and before any provider round trip; `main.tsx` uses it to narrow the splash status line from "Loading…" to "Signing in…", so an auth-less deployment is never told it is signing in. It resolves to an `AuthFailure | null` (see `authFailure.ts`): a non-null result means the sign-in cannot be completed, and `main.tsx` then leaves React-admin unmounted and shows the reason on the splash instead — mounting it would re-run `checkAuth` and head straight back to the provider that just failed. A `/api/v1/config` request that merely fails resolves `null` on purpose, so a restarting backend still renders the app.
- **Pre-mount loading screen (`src/layout/AppSplash.tsx`)** – covers the window between page load and the React-admin mount, which is otherwise blank while `bootstrapAuth()` resolves the session. It brings its own dark theme and issues no requests: it must render outside `AppProviders`, whose deploy-lock and ArgoCD-status providers start polling the API on mount, before any token exists. Given an `error` prop it becomes the failure surface as well: the progress dots stop and an error box below the logo panel names the reason — the provider's own error code and description, or the discovery URL the browser could not read — with a **Try again** button as the only retry, since redirecting on its own is the loop this replaces.
- **OIDC toggle hook (`src/shared/hooks/useOidcEnabled.ts`)** – tiny helper for gating UI affordances when running without identity.
- **Live status services (`src/data/wsStatusService.ts`)** – shared base for a server-owned signal the UI mirrors: bootstrap over REST, then track transitions pushed on the `/ws` channel, with one socket per signal while listeners exist, reconnect-and-reconcile when the socket drops, and ordering guards so a slow REST response cannot overwrite a fresher push. `deployLockService` (`src/features/deployLock/`) extends it and adds the imperative lock/release operations; `argocdStatusService` (`src/features/argocdStatus/`) extends it read-only for ArgoCD reachability.
- **Global providers (`src/shared/providers/AppProviders.tsx`)** – wraps the app with the theme mode, timezone, and deploy-lock providers, plus a global banner that reflects lock status.

## Shared UX Infrastructure

- `ThemeModeProvider` persists light/dark preference in `localStorage`, syncs the `<html data-theme-mode>` attribute, and exposes `useThemeMode`.
- `TimezoneProvider` stores the preferred timezone (`utc` vs `local`) and exposes helpers for deterministic formatting (`formatDateTime` lives in `shared/utils/time.ts`).
- `layout/components` contains building blocks for notifications, navigation, and placeholders—extend these instead of forking React-admin defaults.
- Feature folders follow the “colocate everything” pattern (components, hooks, tests alongside their feature) to keep future phases modular.

## Testing & Quality Gates

- Vitest runs in `jsdom` with globals + `vitest.setup.ts` (place shared mocks, `@testing-library/jest-dom`, etc.).
- Tests live next to the code as `*.test.ts(x)` and are auto-discovered via `vitest.config.ts`.
- Coverage reporters: text summary in CI plus LCOV for Codecov. Keep critical flows (auth provider, data provider, the shared WS status protocol, utilities) covered.
- oxlint enforces React, hooks rules, and TypeScript correctness. Configure additional lint rules under `.oxlintrc.json` if new conventions emerge.

### Browser tests (Playwright)

`e2e/` covers what jsdom structurally cannot: real cross-document navigation, a
real history stack, and a real WebSocket. Everything a component test can assert
belongs in Vitest — this suite is deliberately small.

Run it with `task test-web-e2e` from the repo root; `playwright.config.ts` starts
what it drives (the mock Argo CD API and two `argo-watcher` instances serving the
built bundle, on 8100 without OIDC and 8101 with), but Keycloak comes from the
`integration` Compose profile and must already be up.

| Project | Covers |
| ------- | ------ |
| `no-auth` | The server's SPA fallback for a deep-linked task URL; Back from a directly-opened task page, where the detail view is the router's first history entry; and the auth-less deployment mode, which must render with no sign-in and offer no privileged controls. |
| `auth` | The top-level OIDC redirect and the stripping of its callback query; returning to the linked task via `url_state`; session recovery through the SSO cookie after a reload; the deploy-lock banner arriving over the live socket and the lock being toggled from the drawer; rollback as a privileged user; and privilege gating of both controls for a privileged vs a regular realm user. |

The Keycloak client in `test/keycloak/argo-watcher-e2e-realm.json` has the
standard (authorization-code) flow enabled with the app's exact redirect URI
`http://localhost:8101/` registered, which is what lets a browser complete the
flow, and it requires PKCE (`S256`) — so a regression that stopped sending a code
challenge fails every auth spec. The Go integration tests use the same client
through the direct access grant, so both stay enabled.

Two constraints when adding specs:

- `@playwright/test` is pinned to the exact version of `playwright-driver` in
  `flake.nix`, which supplies the browsers in the dev shell. Bump them together.
- Both servers hold in-memory state that every spec seeds into, so the suite runs
  single-worker. Give each seeded task a distinct author rather than assuming an
  empty list, and never wait on the task table to decide the app has loaded — an
  empty list has no column headers, so such a wait passes only when some earlier
  spec happened to seed first. Use `expectAppLoaded` instead.

## Building & Shipping

1. Run `npm run build` from `web/`.
2. The output in `web/dist` is what the Go server serves when `STATIC_FILES_PATH` points to this directory (default). The Dockerfiles already copy `web/dist` into the container image during CI.
3. If you need to publish the frontend separately (e.g., to an object store), upload the contents of `dist/` and set `VITE_API_BASE_URL`/`VITE_WS_BASE_URL` accordingly before building so the SPA calls the correct origin.

## Extending the Frontend

1. **Add new resources** – create a folder under `src/features/<resource>` with its pages, register the resource in `App.tsx`, and extend `dataProvider`.
2. **Integrate new API endpoints** – implement helpers in `src/data/httpClient.ts` or compose smaller services similar to `deployLockService`.
3. **UI building blocks** – prefer MUI components themed through `theme/index.ts`. Keep styling in Emotion for server-render compatibility down the line.

## Troubleshooting

- **Dev server cannot reach the API**: ensure `VITE_API_PROXY_TARGET` matches your backend host or export `VITE_API_BASE_URL` so the SPA calls the right origin.
- **Endless OIDC redirects**: verify the provider's client lists the current app origin (including any base path) as a valid redirect URI. The login flow uses a top-level redirect, so the redirect URI must be allow-listed there. If the redirect URIs are correct and the loop only appears *after* a successful login, the callback query (`?code=…&state=…`) was stripped before `oidc-client-ts` could read it — this is why `bootstrapAuth()` must be awaited in `main.tsx` before React mounts; do not move the callback handling back into the React tree.
- **WebSocket errors**: set `VITE_WS_BASE_URL` when proxying through TLS terminators that do not support upgrade requests, or confirm `/ws` is exposed by the Go server.
- **Timezones look wrong**: toggle the timezone via the user menu (wired to `TimezoneProvider`). The selection lives under `argo-watcher:timezone`.

This README should stay exhaustive—update it whenever you add scripts, env vars, or architectural pieces so contributors can onboard without spelunking through the codebase.
