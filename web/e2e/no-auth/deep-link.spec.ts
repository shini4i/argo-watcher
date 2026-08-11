import { expect, test } from '@playwright/test';
import { seedTask, waitForDeployed } from '../helpers';

/**
 * Deep-linking a task detail page exercises the Go server's SPA fallback
 * (internal/server/staticfs.go serves index.html for unmatched paths) together
 * with the client router picking the route back up. jsdom tests mount the router
 * directly and never issue that request, so this path is only covered here.
 */
test.describe('task detail deep link', () => {
  test('the server answers an unknown client route with the SPA shell', async ({ request }) => {
    const response = await request.get('/task/does-not-exist');

    expect(response.status()).toBe(200);
    expect(response.headers()['content-type']).toContain('text/html');
    expect(await response.text()).toContain('<div id="root">');
  });

  test('opening a task URL directly renders that task', async ({ page, request }) => {
    const id = await seedTask(request, 'deep-link');
    await waitForDeployed(request, id);

    await page.goto(`/task/${id}`);

    await expect(page.getByText(`Task ${id.slice(0, 8)}`)).toBeVisible();
    // Exact: "Rollback to this version" also contains "Back".
    await expect(page.getByRole('button', { name: 'Back', exact: true })).toBeVisible();
  });

  /**
   * Regression cover for #536. On a directly-opened task URL the detail page is
   * the router's FIRST history entry, so a plain `navigate(-1)` steps out of the
   * SPA entirely instead of moving within it. Only a real browser has a history
   * stack that can be stepped off the end of.
   */
  test('Back from a directly-opened task page stays in the app and shows the list', async ({ page, request }) => {
    const id = await seedTask(request, 'deep-link-back');
    await waitForDeployed(request, id);

    await page.goto(`/task/${id}`);
    await expect(page.getByText(`Task ${id.slice(0, 8)}`)).toBeVisible();

    await page.getByRole('button', { name: 'Back', exact: true }).click();

    // react-admin appends its own list query (sort, page, filters) to /tasks.
    await expect(page).toHaveURL(/\/tasks(\?|$)/);
    await expect(page.getByRole('columnheader', { name: 'Application' })).toBeVisible();
  });
});
