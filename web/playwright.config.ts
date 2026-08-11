import { existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { defineConfig, devices } from '@playwright/test';

// 8100/8101 are private to this suite, chosen to miss the docker-compose dev
// stack (8080 backend, 8090 keycloak, 3100 frontend). 8081 is NOT private: it is
// the dev stack's mock port, shared because cmd/mock hardcodes its listen
// address. Locally a running dev stack's mock is reused rather than started; in
// CI (reuseExistingServer false) anything already holding 8081 aborts the run.
const NO_AUTH_PORT = 8100;
const AUTH_PORT = 8101;
const MOCK_PORT = 8081;

const NO_AUTH_URL = `http://localhost:${NO_AUTH_PORT}`;
const AUTH_URL = `http://localhost:${AUTH_PORT}`;

// Keycloak from the compose `integration` profile. The suite does not start it:
// compose has no health check for this image, so the caller (task test-web-e2e,
// or the web-e2e workflow) brings it up and polls the realm's discovery endpoint
// before invoking Playwright.
const KEYCLOAK_REALM_URL = 'http://localhost:8090/realms/argo-watcher-e2e';

// The suite drives the shipped artifact shape: the Go server serving web/dist as
// static files, exactly as the production image does. A missing bundle would
// otherwise surface as a wall of unrelated selector failures.
// Resolved against this file, not the process CWD, so the guard reports the real
// problem regardless of where Playwright was invoked from.
if (!existsSync(fileURLToPath(new URL('dist/index.html', import.meta.url)))) {
  throw new Error('web/dist/index.html not found. Run "task build-ui" before the e2e suite.');
}

const serverEnv = {
  STATE_TYPE: 'in-memory',
  // The mock Argo CD API (cmd/mock) reports app "app" as Synced/Healthy running
  // app:v0.0.1, which is what lets a seeded task reach "deployed" deterministically.
  ARGO_URL: `http://localhost:${MOCK_PORT}`,
  ARGO_TOKEN: 'e2e',
  STATIC_FILES_PATH: 'web/dist',
  LOG_LEVEL: 'warn',
};

export default defineConfig({
  testDir: './e2e',
  // Both servers hold in-memory state that every spec seeds into, so tests are
  // serialized rather than racing each other's task lists.
  workers: 1,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  timeout: 60_000,
  expect: { timeout: 15_000 },
  reporter: process.env.CI
    ? [['list'], ['html', { open: 'never' }], ['github']]
    : [['list'], ['html', { open: 'never' }]],
  use: {
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off',
  },

  projects: [
    {
      name: 'no-auth',
      testDir: './e2e/no-auth',
      use: { ...devices['Desktop Chrome'], baseURL: NO_AUTH_URL },
    },
    {
      name: 'auth',
      testDir: './e2e/auth',
      use: {
        ...devices['Desktop Chrome'],
        baseURL: AUTH_URL,
      },
    },
  ],

  webServer: [
    {
      name: 'mock-argocd',
      command: 'go run ./cmd/mock',
      cwd: '..',
      url: `http://localhost:${MOCK_PORT}/api/v1/applications/app`,
      reuseExistingServer: !process.env.CI,
      timeout: 180_000,
      stdout: 'ignore',
    },
    {
      name: 'argo-watcher',
      command: 'go run ./cmd/argo-watcher',
      cwd: '..',
      // Liveness, never readiness: /readyz is legitimately 503 while a state
      // backend is unreachable, and the lab's e2e scripts gate on /livez for the
      // same reason.
      url: `${NO_AUTH_URL}/livez`,
      reuseExistingServer: !process.env.CI,
      timeout: 180_000,
      stdout: 'ignore',
      env: { ...serverEnv, PORT: String(NO_AUTH_PORT) },
    },
    {
      name: 'argo-watcher-oidc',
      command: 'go run ./cmd/argo-watcher',
      cwd: '..',
      url: `${AUTH_URL}/livez`,
      reuseExistingServer: !process.env.CI,
      timeout: 180_000,
      stdout: 'ignore',
      env: {
        ...serverEnv,
        PORT: String(AUTH_PORT),
        OIDC_ENABLED: 'true',
        OIDC_ISSUER_URL: KEYCLOAK_REALM_URL,
        OIDC_CLIENT_ID: 'argo-watcher',
        OIDC_PRIVILEGED_GROUPS: 'privileged',
      },
    },
  ],
});
