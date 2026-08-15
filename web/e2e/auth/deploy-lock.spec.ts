import { expect, test, type Page } from '@playwright/test';
import { expectAppLoaded, KEYCLOAK_ORIGIN, mintToken, signIn } from '../helpers';

const BANNER = /Deploy lock is active/;
const LOCK_SWITCH = 'Toggle deploy lock';

const signInPrivileged = async (page: Page): Promise<void> => {
  await page.goto('/');
  await page.waitForURL(url => url.href.startsWith(KEYCLOAK_ORIGIN));
  await signIn(page);
  await expectAppLoaded(page);
};

test.describe('deploy lock', () => {
  // The lock is server-global and the servers outlive each test, so a failure
  // part-way through either test must not leave it engaged for the retry or the
  // next spec.
  test.afterEach(async ({ request }) => {
    const token = await mintToken(request);
    await request.delete('/api/v1/deploy-lock', { headers: { 'Oidc-Authorization': `Bearer ${token}` } });
  });

  /**
   * The banner is driven by the server's WebSocket, which the unit suite stubs.
   * Locking through the API and watching an already-open page react drives the
   * real chain: a handshake from the built bundle, the server's lock watcher
   * noticing on its poll, and the push landing with no reload.
   */
  test('the banner appears and clears from the live socket', async ({ page, request }) => {
    const token = await mintToken(request);
    const authHeader = { 'Oidc-Authorization': `Bearer ${token}` };

    await signInPrivileged(page);
    await expect(page.getByText(BANNER)).toHaveCount(0);

    const locked = await request.post('/api/v1/deploy-lock', { headers: authHeader });
    expect(locked.status()).toBe(200);

    // The server samples the lock every 5s and pushes on transition, so this
    // waits on a broadcast rather than on a client-side poll.
    await expect(page.getByText(BANNER)).toBeVisible({ timeout: 30_000 });

    const released = await request.delete('/api/v1/deploy-lock', { headers: authHeader });
    expect(released.status()).toBe(200);

    await expect(page.getByText(BANNER)).toHaveCount(0, { timeout: 30_000 });
  });

  /**
   * The other direction: locking from the drawer. This is the only place the
   * browser's in-memory access token is proven to reach a privileged write —
   * component tests stub the HTTP client and never produce a real token.
   */
  test('a privileged user can engage and release the lock from the drawer', async ({ page, request }) => {
    await signInPrivileged(page);

    await page.getByRole('button', { name: 'Open configuration drawer' }).click();
    await expect(page.getByRole('heading', { name: 'Workspace Controls' })).toBeVisible();
    await expect(page.getByText('Lock released')).toBeVisible();

    await page.getByLabel(LOCK_SWITCH).click();

    await expect(page.getByText('Lock engaged')).toBeVisible();
    const token = await mintToken(request);
    const authHeader = { 'Oidc-Authorization': `Bearer ${token}` };
    await expect
      .poll(async () => (await request.get('/api/v1/deploy-lock', { headers: authHeader })).text(), {
        message: 'the server must report the lock the UI just set',
      })
      .toContain('true');

    await page.getByLabel(LOCK_SWITCH).click();

    await expect(page.getByText('Lock released')).toBeVisible();
    await expect
      .poll(async () => (await request.get('/api/v1/deploy-lock', { headers: authHeader })).text(), {
        message: 'the server must report the lock the UI just released',
      })
      .toContain('false');
  });
});
