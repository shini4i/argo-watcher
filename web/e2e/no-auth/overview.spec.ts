import { expect, test } from '@playwright/test';
import { MOCK_APP, seedTask, waitForDeployed } from '../helpers';

/**
 * @description The overview is the only screen served by
 * `/api/v1/apps/summary`, and the unit suite stubs that fetch. Only a browser
 * against the real server proves the endpoint exists, that its aggregate shape
 * is the one the page reads, and that a row links to that app's History over
 * the counted window.
 *
 * The servers keep in-memory state across specs, so counts here are lower
 * bounds: other specs' seeds share this window.
 */
test('the overview summarises the window and links each app to its history', async ({ page, request }) => {
  const id = await seedTask(request, 'overview');
  await waitForDeployed(request, id);

  await page.goto('/');
  await page.getByRole('link', { name: 'Overview' }).click();
  await expect(page).toHaveURL(/\/overview$/);

  // The strip shows skeletons until the summary resolves, so a real number here
  // is what proves the endpoint answered with usable data.
  const deployed = page.locator('div', { has: page.getByText('DEPLOYED', { exact: true }) }).last();
  await expect(deployed).toContainText(/\b[1-9]\d*\b/);

  const row = page.getByRole('link', { name: new RegExp(`^${MOCK_APP}\\b`) });
  await expect(row).toBeVisible();

  // Client-side filter over the same rows: an impossible needle must empty the
  // list rather than leave it unfiltered.
  await page.getByLabel('Filter applications').fill('no-such-application');
  await expect(page.getByText(/No application matches/)).toBeVisible();
  await page.getByLabel('Filter applications').fill('');

  // History, not Recent Tasks: the link carries the window the overview
  // counted, and the task seeded above must be listed within it.
  await row.click();
  await expect(page).toHaveURL(/\/history\?/);
  const params = new URL(page.url()).searchParams;
  expect(params.get('app')).toBe(MOCK_APP);
  expect(Number(params.get('endDate')) - Number(params.get('startDate'))).toBe(24 * 60 * 60);
  await expect(page.getByText('overview', { exact: true }).first()).toBeVisible();
});
