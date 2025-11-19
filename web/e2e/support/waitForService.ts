const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));

export const waitForService = async (url: string, {
  timeoutMs = 60_000,
  pollIntervalMs = 2_000,
  expectStatus = 200,
}: {
  timeoutMs?: number;
  pollIntervalMs?: number;
  expectStatus?: number;
} = {}) => {
  const deadline = Date.now() + timeoutMs;

  let lastError: Error | null = null;

  while (Date.now() < deadline) {
    try {
      const response = await fetch(url, { method: 'GET' });
      if (!response.ok && response.status !== expectStatus) {
        lastError = new Error(`Unexpected status ${response.status} for ${url}`);
      } else {
        return;
      }
    } catch (error) {
      lastError = error as Error;
    }
    await sleep(pollIntervalMs);
  }

  const reason = lastError?.message ?? 'timeout exceeded';
  throw new Error(`Timed out waiting for ${url}: ${reason}`);
};
