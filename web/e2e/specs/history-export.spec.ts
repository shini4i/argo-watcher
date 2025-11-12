import { test, expect } from '@playwright/test';
import { promises as fs } from 'node:fs';
import path from 'node:path';
import { captureScreenshot } from '../fixtures/screenshots';
import { resetState, seedHistoryTasks } from '../fixtures/seed';

test.describe('History view export', () => {
  test.beforeEach(async () => {
    await resetState();
    await seedHistoryTasks();
  });

  test('filters by date range and downloads anonymized JSON', async ({ page }, testInfo) => {
    const startDate = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000);
    const endDate = new Date();
    const startValue = startDate.toISOString().slice(0, 10);
    const endValue = endDate.toISOString().slice(0, 10);

    await page.goto(`/history?startDate=${startValue}&endDate=${endValue}`);
    await page.waitForResponse(response => response.url().includes('/api/v1/tasks') && response.request().method() === 'GET');

    const rows = page.getByRole('rowgroup').nth(1).getByRole('row');
    await expect(rows.filter({ hasText: /history-app-alpha/i })).toHaveCount(1, { timeout: 15000 });

    const downloadsDir = testInfo.outputDir;

    await page.getByRole('button', { name: 'Export' }).click();
    const [download] = await Promise.all([
      page.waitForEvent('download'),
      page.getByRole('menuitem', { name: 'Download JSON' }).click(),
    ]);

    const filePath = path.join(downloadsDir, await download.suggestedFilename());
    await download.saveAs(filePath);
    const raw = await fs.readFile(filePath, 'utf-8');
    const parsed = JSON.parse(raw) as Array<Record<string, unknown>>;
    expect(Array.isArray(parsed)).toBe(true);
    expect(parsed[0]?.app).toBeDefined();
    expect(parsed[0]?.author).toBeUndefined();

    await captureScreenshot(page, testInfo, 'history-export');
  });
});
