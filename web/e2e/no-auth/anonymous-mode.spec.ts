import { expect, test } from '@playwright/test';
import { seedTask, waitForDeployed } from '../helpers';

/**
 * The auth-less deployment (`oidc.enabled = false`) is a supported mode, and it
 * must render with no sign-in and no privileged controls. The gating reads
 * `/api/v1/config` from the real server, so only a browser against a real
 * OIDC-disabled instance proves the app agrees with what the backend reports.
 */
test('an anonymous deployment renders without a sign-in and hides privileged controls', async ({ page, request }) => {
  const id = await seedTask(request, 'anonymous-mode');
  await waitForDeployed(request, id);

  await page.goto(`/task/${id}`);

  await expect(page.getByText(`Task ${id.slice(0, 8)}`)).toBeVisible();

  await page.getByRole('button', { name: 'Open configuration drawer' }).click();
  await expect(page.getByRole('heading', { name: 'Workspace Controls' })).toBeVisible();
  // Assert this first: it is the only marker that appears once /api/v1/config has
  // actually resolved to disabled. The absences below are also true while the
  // config is still in flight, so checking them earlier would prove nothing.
  await expect(page.getByText('Manual deploy lock requires authentication.')).toBeVisible();
  // The toggle is not merely disabled here: without OIDC there is no identity to
  // authorize, so the control is not offered at all.
  await expect(page.getByLabel('Toggle deploy lock')).toHaveCount(0);
  // Same reason there is no identity to authorize: there is none to name either.
  // Keys on the account card's menu trigger, the one element the badge always renders.
  await expect(page.locator('[aria-haspopup="menu"]')).toHaveCount(0);
  await expect(page.getByRole('img', { name: 'Privileged access' })).toHaveCount(0);

  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: 'Rollback to this version' })).toHaveCount(0);
});
