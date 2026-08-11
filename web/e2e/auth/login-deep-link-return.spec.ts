import { expect, test } from '@playwright/test';
import { KEYCLOAK_ORIGIN, seedTask, signIn, waitForDeployed } from '../helpers';

/**
 * Signing in from a shared task link must land back on that task, not on the
 * app's default screen. The path survives the provider round trip in `url_state`,
 * and losing it is what turned a shared link into the relogin loop of #536.
 *
 * The round trip is a real cross-document navigation, so jsdom cannot cover it.
 */
test('signing in from a shared task link returns to that task', async ({ page, request }) => {
  const id = await seedTask(request, 'login-deep-link-return');
  await waitForDeployed(request, id);

  await page.goto(`/task/${id}`);
  await page.waitForURL(url => url.href.startsWith(KEYCLOAK_ORIGIN));
  await signIn(page);

  await expect(page).toHaveURL(new RegExp(`/task/${id}$`));
  await expect(page.getByText(`Task ${id.slice(0, 8)}`)).toBeVisible();
});
