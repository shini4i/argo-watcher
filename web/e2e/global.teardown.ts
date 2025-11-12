import { setDeployLock } from './fixtures/apiClient';

export default async function globalTeardown() {
  await setDeployLock(false);
}
