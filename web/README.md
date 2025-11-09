# Argo Watcher React-admin Frontend

This workspace contains the Vite/React-admin rewrite of the Argo Watcher UI. The goals of the new shell are:

- reduce custom dashboard code in favour of React-admin resources and generators
- share layouts, deploy-lock state, and theming logic with the Go backend
- keep the developer workflow fast (Vite + HMR) while still producing static assets that the Go server can embed

React 18, React-admin 5, Material UI 5, Emotion, TypeScript, ESLint, and Vitest/JSDOM are the primary dependencies. Keycloak is the supported identity provider, with fully anonymous mode when the backend returns `keycloak.enabled = false`.

## Directory Layout

| Path | Purpose |
| ---- | ------- |
| `src/main.tsx` | React entry point that wires React Router, shared providers, and the `App` component. |
| `src/App.tsx` | Placeholder React-admin shell; extend this file with `<Admin>` resources as migration phases land. |
| `src/auth/` | Authentication helpers. `authProvider.ts` talks to Keycloak + `/api/v1/config`, and `tokenStore.ts` persists bearer tokens. |
| `src/data/` | HTTP layer and React-admin `dataProvider` implementation that targets `/api/v1/tasks` and related endpoints. |
| `src/features/` | Feature modules (currently `tasks/` and `deployLock/`). Each folder owns its components, hooks, and service logic. |
| `src/layout/` | Reusable React-admin layout primitives (notifications, top bar, nav, etc.). |
| `src/shared/` | Cross-cutting hooks, context providers (timezone), and utilities (time formatting, Keycloak toggles). |
| `src/theme/` | Material UI theme factory plus `ThemeModeProvider` for light/dark persistence. |
| `assets/` + `public/` | Static assets (logos, favicons) delivered verbatim by Vite. |
| `dist/` | Build output consumed by the Go server (`STATIC_FILES_PATH`) or Docker images. Generated via `npm run build`. |

## Environment & Runtime Configuration

All configuration is driven through the backend plus a small set of Vite runtime variables:

| Variable | Default | Usage |
| -------- | ------- | ----- |
| `VITE_API_PROXY_TARGET` | `http://localhost:8080` | Dev-only proxy target for `/api` + `/ws` when running `npm run dev`. |
| `VITE_API_BASE_URL` | `''` (same origin) | Prepended to every REST call executed by `httpClient`. Set this when serving the SPA from a CDN or different domain. |
| `VITE_WS_BASE_URL` | `''` (derived from `window.location`) | Optional WebSocket origin for the deploy-lock service. Provide when tunneling through another host. |

Keycloak settings (URL, realm, client_id, privileged groups, token intervals) come from `/api/v1/config`. The frontend caches the config and only attempts SSO flows when `keycloak.enabled` is true.

## Development Workflow

1. **Enter the dev shell (recommended)**  
   ```bash
   nix develop        # or direnv allow (uses flake.nix to expose Go + Node toolchains)
   ```
   The shell exports `nodejs@20`, pnpm/corepack helpers, Vite shim, Go toolchain, and pre-commit hooks.

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
| `npm run lint` | ESLint (`@typescript-eslint`, React, hooks, prettier config). |
| `npm run test` | Vitest in CI mode (`--run`). Generates text + LCOV coverage. |
| `npm run test:watch` | Interactive Vitest watch mode. |
| `npm run test:ui` | Vitest UI (requires a browser) for debugging complex suites. |

## Data, Auth, and Real-time Architecture

- **HTTP client (`src/data/httpClient.ts`)** – wraps `fetch`, injects `Authorization`/`Keycloak-Authorization` headers from `tokenStore`, normalises errors into `HttpError`, and provides helpers such as `buildQueryString`.
- **React-admin `dataProvider` (`src/data/dataProvider.ts`)** – currently implements the `tasks` resource (list/detail/create) and infers pagination totals when the backend omits them. Extend this file when exposing additional Argo watcher endpoints.
- **Auth provider (`src/auth/authProvider.ts`)** – lazy-loads `/api/v1/config`, boots Keycloak when enabled, caches silent SSO preferences to avoid redirect loops, periodically refreshes tokens, and exposes permissions (groups/privileged groups) to React-admin.
- **Keycloak toggle hook (`src/shared/hooks/useKeycloakEnabled.ts`)** – tiny helper for gating UI affordances when running without identity.
- **Deploy lock service (`src/features/deployLock/deployLockService.ts`)** – shares lock state through REST endpoints plus the `/ws` channel. Automatic retries and subscriber management keep the UI in sync even if the socket drops.
- **Global providers (`src/shared/providers/AppProviders.tsx`)** – wraps the app with the theme mode, timezone, and deploy-lock providers, plus a global banner that reflects lock status.

## Shared UX Infrastructure

- `ThemeModeProvider` persists light/dark preference in `localStorage`, syncs the `<html data-theme-mode>` attribute, and exposes `useThemeMode`.
- `TimezoneProvider` stores the preferred timezone (`utc` vs `local`) and exposes helpers for deterministic formatting (`formatDateTime` lives in `shared/utils/time.ts`).
- `layout/components` contains building blocks for notifications, navigation, and placeholders—extend these instead of forking React-admin defaults.
- Feature folders follow the “colocate everything” pattern (components, hooks, tests alongside their feature) to keep future phases modular.

## Testing & Quality Gates

- Vitest runs in `jsdom` with globals + `vitest.setup.ts` (place shared mocks, `@testing-library/jest-dom`, etc.).  
- Tests live next to the code as `*.test.ts(x)` and are auto-discovered via `vitest.config.ts`.  
- Coverage reporters: text summary in CI plus LCOV for Codecov. Keep critical flows (auth provider, data provider, deploy-lock logic, utilities) covered.  
- ESLint enforces React 18, hooks rules, and TypeScript strictness. Configure additional lint rules under `.eslintrc.cjs` (pending) if new conventions emerge.

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
- **Endless Keycloak redirects**: clear `localStorage` key `argo-watcher:silent-sso-disabled` and verify the backend Keycloak config exposes the current redirect URI.  
- **WebSocket errors**: set `VITE_WS_BASE_URL` when proxying through TLS terminators that do not support upgrade requests, or confirm `/ws` is exposed by the Go server.  
- **Timezones look wrong**: toggle the timezone via the user menu (wired to `TimezoneProvider`). The selection lives under `argo-watcher:timezone`.

This README should stay exhaustive—update it whenever you add scripts, env vars, or architectural pieces so contributors can onboard without spelunking through the codebase.
