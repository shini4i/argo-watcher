import { expect, test } from '@playwright/test';
import { expectAppLoaded, KEYCLOAK_ORIGIN, mintToken, seedTask, signIn, waitForDeployed } from '../helpers';

/**
 * The only flow that proves the author on the resulting task comes from the
 * real OIDC identity — component tests stub both the HTTP client and the
 * identity, so neither the token nor the author is real there.
 */
test('a privileged user can roll a task back, and the new task carries their identity', async ({ page, request }) => {
  const id = await seedTask(request, 'rollback-source');
  await waitForDeployed(request, id);

  await page.goto(`/task/${id}`);
  await page.waitForURL(url => url.href.startsWith(KEYCLOAK_ORIGIN));
  await signIn(page);

  await page.getByRole('button', { name: 'Rollback to this version' }).click();
  await expect(page.getByRole('heading', { name: 'Rollback Confirmation' })).toBeVisible();

  // Scope the check below to tasks created from here on. The servers keep their
  // in-memory state across CI retries, so an unscoped search would find the task
  // a previous attempt already created and pass without this click doing anything.
  // Minus one second because task timestamps are whole seconds and the filter's
  // lower bound is exclusive, which would otherwise drop a same-second task.
  const since = Math.floor(Date.now() / 1000) - 1;
  await page.getByRole('button', { name: 'Yes' }).click();

  await expectAppLoaded(page);
  await expect(page).toHaveURL(/\/tasks(\?|$)/);

  const token = await mintToken(request);
  await expect
    .poll(
      async () => {
        const response = await request.get(`/api/v1/tasks?from_timestamp=${since}`, {
          headers: { 'Oidc-Authorization': `Bearer ${token}` },
        });
        const body = (await response.json()) as { tasks?: { author?: string }[] };
        return (body.tasks ?? []).map(task => task.author);
      },
      { message: 'the rollback must create a task authored by the signed-in user' },
    )
    .toContain('priv-user@example.com');
});
