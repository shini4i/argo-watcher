import { e2eEnv } from './config/env';
import { resetState } from './fixtures/seed';
import { waitForService } from './support/waitForService';

export default async function globalSetup() {
  await waitForService(new URL(e2eEnv.healthEndpoint, e2eEnv.apiBaseUrl).toString());
  await waitForService(e2eEnv.webBaseUrl);
  await resetState();
}
