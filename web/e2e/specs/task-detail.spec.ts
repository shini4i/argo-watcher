import { test, expect } from '@playwright/test';
import { captureScreenshot } from '../fixtures/screenshots';
import { resetState, seedTaskDetail } from '../fixtures/seed';

test.describe('Task detail view', () => {
  test.beforeEach(async () => {
    await resetState();
  });

  test('renders timeline, metadata, and status reason', async ({ page }, testInfo) => {
    const task = await seedTaskDetail();
    await page.goto(`/task/${task.id}`);
    await page.waitForResponse(response => response.url().includes(`/api/v1/tasks/${task.id}`));

    await expect(
      page.getByRole('heading', { name: new RegExp(`Task ${task.id.slice(0, 8)}`, 'i') }),
    ).toBeVisible({ timeout: 15000 });
    await expect(page.getByText('Application')).toBeVisible();
    await expect(page.getByText(task.app)).toBeVisible();
    await expect(page.getByText('Project')).toBeVisible();
    await expect(page.getByRole('link', { name: /github\.com\/shini4i\/argo-watcher/i })).toBeVisible();
    await expect(page.getByText('Timeline')).toBeVisible();
    await expect(page.getByText(/Sync failed due to health check timeout/i)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Open in Argo CD UI' })).toBeDisabled();
    await captureScreenshot(page, testInfo, 'task-detail');
  });
});
