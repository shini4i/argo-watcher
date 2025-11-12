import { test, expect } from '@playwright/test';
import { captureScreenshot } from '../fixtures/screenshots';
import { resetState, seedRecentTasks } from '../fixtures/seed';

test.describe('Recent tasks list', () => {
  test.beforeEach(async () => {
    await resetState();
  });

  test('renders seeded tasks and expands status details', async ({ page }, testInfo) => {
    const tasks = await seedRecentTasks();
    await page.goto('/');
    await page.waitForResponse(response => response.url().includes('/api/v1/tasks') && response.request().method() === 'GET');

    await expect(page).toHaveTitle(/Recent Tasks — Argo Watcher/);

    const grid = page.getByRole('grid');
    await expect(grid).toBeVisible({ timeout: 15000 });
    const tableBody = grid.getByRole('rowgroup').nth(1);
    await expect(tableBody.getByRole('row')).toHaveCount(tasks.length, { timeout: 15000 });

    for (const task of tasks) {
      await expect(tableBody.getByRole('row', { name: new RegExp(task.app, 'i') })).toBeVisible();
    }

    const failingTask = tasks.find(task => task.status === 'failed');
    if (!failingTask) {
      throw new Error('Failed to seed mock failure task');
    }

    await page.getByRole('row', { name: new RegExp(failingTask.app, 'i') }).click();
    await expect(page.getByText(failingTask.statusReason ?? '', { exact: false })).toBeVisible();

    await captureScreenshot(page, testInfo, 'recent-tasks');
  });
});
