import { test, expect } from '@playwright/test';
import { captureScreenshot } from '../fixtures/screenshots';
import { resetState, seedTaskDetail } from '../fixtures/seed';

test.describe('Task detail view', () => {
  test.beforeEach(async () => {
    await resetState();
  });

  test('renders timeline, metadata, and status reason', async ({ page }, testInfo) => {
    const task = await seedTaskDetail();
    const taskFetch = page.waitForResponse(response => {
      return response.url().includes(`/api/v1/tasks/${task.id}`) && response.request().method() === 'GET';
    });
    await page.goto(`/task/${task.id}`);
    await taskFetch;

    await expect(page.getByText(new RegExp(`Task ${task.id.slice(0, 8)}`, 'i'))).toBeVisible({
      timeout: 15000,
    });
    await expect(page.getByText('Application')).toBeVisible();
    await expect(page.getByText(task.app)).toBeVisible();
    await expect(page.getByText('Project')).toBeVisible();
    await expect(page.getByRole('link', { name: /github\.com\/shini4i\/argo-watcher/i })).toBeVisible();
    await expect(page.getByText('Timeline')).toBeVisible();
    await expect(page.getByText(/Sync failed due to health check timeout/i)).toBeVisible();
    const argoLink = page.getByRole('link', { name: 'Open in Argo CD UI' });
    await expect(argoLink).toBeVisible();
    await expect(argoLink).toHaveAttribute('href', /detail-app/);
    await captureScreenshot(page, testInfo, 'task-detail');
  });
});
