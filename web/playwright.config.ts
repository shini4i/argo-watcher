import { defineConfig, devices } from '@playwright/test';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const webBaseUrl = process.env.WEB_BASE_URL ?? 'http://localhost:3100';
const apiBaseUrl = process.env.API_BASE_URL ?? 'http://localhost:8080';

const artifactsDir = path.join(__dirname, 'e2e', 'artifacts');

export default defineConfig({
  testDir: path.join(__dirname, 'e2e', 'specs'),
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  timeout: 60_000,
  expect: {
    timeout: 10_000,
  },
  outputDir: path.join(artifactsDir, 'results'),
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: path.join(artifactsDir, 'report') }],
  ],
  use: {
    baseURL: webBaseUrl,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    viewport: { width: 1440, height: 900 },
    actionTimeout: 10_000,
    navigationTimeout: 20_000,
  },
  metadata: {
    webBaseUrl,
    apiBaseUrl,
  },
  globalSetup: path.join(__dirname, 'e2e', 'global.setup.ts'),
  globalTeardown: path.join(__dirname, 'e2e', 'global.teardown.ts'),
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
      },
    },
  ],
});
