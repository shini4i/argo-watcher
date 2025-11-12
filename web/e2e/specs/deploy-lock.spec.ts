import { test, expect } from '@playwright/test';
import { captureScreenshot } from '../fixtures/screenshots';
import { resetState } from '../fixtures/seed';
import { setDeployLock } from '../fixtures/apiClient';

test.describe('Deploy lock and configuration drawer', () => {
  test.beforeEach(async () => {
    await resetState();
  });

  test('toggles deploy lock and persists timezone preference', async ({ page }, testInfo) => {
    await setDeployLock(false);
    await page.goto('/');

    await page.getByLabel('Open configuration drawer').click();

    await page.getByRole('button', { name: /Switch to/i }).click();
    await page.getByRole('button', { name: /Local/ }).click();

    await expect(async () => {
      const timezone = await page.evaluate(() => window.localStorage.getItem('argo-watcher:timezone'));
      expect(timezone).toBe('local');
    }).toPass();

    const toggle = page.getByLabel('Toggle deploy lock');
    const banner = page.getByText(/Deploy lock is active/i);

    await expect(toggle).toBeVisible();
    await expect(toggle).toBeEnabled();
    await expect(toggle).not.toBeChecked();

    await toggle.click();
    await expect(toggle).toBeChecked();
    await expect(banner).toBeVisible({ timeout: 10_000 });
    await captureScreenshot(page, testInfo, 'deploy-lock-enabled');

    await expect(toggle).toBeEnabled({ timeout: 10_000 });
    await toggle.click();
    await expect(toggle).not.toBeChecked();
    await expect(banner).not.toBeVisible({ timeout: 10_000 });
  });
});
