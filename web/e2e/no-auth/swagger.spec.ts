import { expect, test } from '@playwright/test';

/**
 * `/swagger/` is served straight out of `web/dist/swagger`, and a request that
 * names no file there falls back to the SPA — so a bundle copied to the wrong
 * path answers with the Web UI at status 200 instead of failing. Only a browser
 * that actually renders the spec proves the page's own assets resolved.
 */
test('the swagger page renders the API specification', async ({ page }) => {
  const pageErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));

  await page.goto('/swagger/');

  await expect(page.getByRole('heading', { name: 'Argo-Watcher API' })).toBeVisible();
  // The spec drives the rendered operations, so one endpoint proves swagger.json
  // was fetched and parsed rather than the shell merely loading.
  await expect(page.getByRole('button', { name: /\/api\/v1\/tasks/ }).first()).toBeVisible();
  expect(pageErrors).toEqual([]);
});
