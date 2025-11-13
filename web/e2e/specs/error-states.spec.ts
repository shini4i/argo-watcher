import { test, expect } from '@playwright/test';
import { captureScreenshot } from '../fixtures/screenshots';
import { resetState } from '../fixtures/seed';

test.describe('Error and empty states', () => {
  test('shows placeholder when no tasks exist', async ({ page }, testInfo) => {
    await resetState();
    await page.goto('/');
    await page.waitForResponse(response => response.url().includes('/api/v1/tasks') && response.request().method() === 'GET');

    await expect(page.getByText('No recent tasks so far…')).toBeVisible({ timeout: 15000 });
    await expect(page.getByText('Auto-refreshing every 15 seconds…')).toBeVisible({ timeout: 15000 });
    await captureScreenshot(page, testInfo, 'no-tasks-placeholder');
  });

  test('surfaces notification when config fetch fails', async ({ page }, testInfo) => {
    const handler = (route: Parameters<Parameters<typeof page.route>[1]>[0]) => {
      route.abort('failed');
    };

    await page.route('**/api/v1/config', handler);

    await page.goto('/');
    await page.getByRole('button', { name: /open configuration drawer/i }).click();

    await expect(page.getByText(/Unable to reach the Argo Watcher API/i)).toBeVisible({ timeout: 15000 });
    await captureScreenshot(page, testInfo, 'config-fetch-error');

    await page.unroute('**/api/v1/config', handler);
  });
});
