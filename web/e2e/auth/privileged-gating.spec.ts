import { expect, test, type Page } from '@playwright/test';
import {
  awaitPermissionsResolved,
  KEYCLOAK_ORIGIN,
  mintToken,
  PRIVILEGED_USER,
  REGULAR_USER,
  seedTask,
  signIn,
  waitForDeployed,
} from '../helpers';

const ROLLBACK_BUTTON = 'Rollback to this version';
const LOCK_SWITCH = 'Toggle deploy lock';

const openConfigDrawer = async (page: Page): Promise<void> => {
  await page.getByRole('button', { name: 'Open configuration drawer' }).click();
  // MUI's temporary Drawer is a presentation modal, not a dialog, so wait on its
  // heading rather than a role.
  await expect(page.getByRole('heading', { name: 'Workspace Controls' })).toBeVisible();
};

/**
 * Opens a task as the given realm user and returns once permissions have
 * resolved, so gating assertions observe the settled state.
 */
const openTaskAs = async (
  page: Page,
  id: string,
  user: { username: string; password: string },
): Promise<void> => {
  await page.goto(`/task/${id}`);
  await page.waitForURL(url => url.href.startsWith(KEYCLOAK_ORIGIN));
  const permissions = awaitPermissionsResolved(page);
  await signIn(page, user.username, user.password);
  await permissions;
  await expect(page.getByText(`Task ${id.slice(0, 8)}`)).toBeVisible();
};

/**
 * The rollback button and the deploy-lock toggle both gate on
 * `permissions.groups`, which the app resolves from the provider's userinfo
 * endpoint — the same source the backend enforces on. Component tests
 * inject that permission object directly, so only a browser signed in against a
 * real provider proves the full chain: token, userinfo call, `groups` claim, and
 * the rendered control. The last assertion in each test pins the documented
 * invariant that the UI's gating agrees with what the server actually allows.
 */
test.describe('privileged control gating', () => {
  test('a privileged user gets both controls, and the server agrees', async ({ page, request }) => {
    const id = await seedTask(request, 'gating-privileged');
    await waitForDeployed(request, id);

    await openTaskAs(page, id, PRIVILEGED_USER);

    await expect(page.getByRole('button', { name: ROLLBACK_BUTTON })).toBeVisible();

    await openConfigDrawer(page);
    await expect(page.getByLabel(LOCK_SWITCH)).toBeEnabled();

    const token = await mintToken(request, PRIVILEGED_USER);
    const locked = await request.post('/api/v1/deploy-lock', {
      headers: { 'Oidc-Authorization': `Bearer ${token}` },
    });
    expect(locked.status(), 'a privileged user may set the lock').toBe(200);
  });

  test('a regular user gets neither control, and the server agrees', async ({ page, request }) => {
    const id = await seedTask(request, 'gating-regular');
    await waitForDeployed(request, id);

    await openTaskAs(page, id, REGULAR_USER);

    // The task itself must still render — this is gating, not a lockout.
    await expect(page.getByRole('button', { name: ROLLBACK_BUTTON })).toHaveCount(0);

    await openConfigDrawer(page);
    await expect(page.getByLabel(LOCK_SWITCH)).toBeDisabled();
    await expect(page.getByText('Deploy lock requires privileged access.')).toBeVisible();

    // 401, not 403: the deploy-lock endpoints answer a valid-but-unprivileged
    // token the same way as no token at all. TestKeycloakDeployLockAuthz owns the
    // exhaustive server-side matrix; this pairs one case with the hidden control.
    const token = await mintToken(request, REGULAR_USER);
    const rejected = await request.post('/api/v1/deploy-lock', {
      headers: { 'Oidc-Authorization': `Bearer ${token}` },
    });
    expect(rejected.status(), 'a regular user must not be able to set the lock').toBe(401);
  });

  // The lock is server-global and the servers outlive each test, so a failure
  // mid-test must not leave it engaged for the retry or the next spec.
  test.afterEach(async ({ request }) => {
    const token = await mintToken(request, PRIVILEGED_USER);
    await request.delete('/api/v1/deploy-lock', { headers: { 'Oidc-Authorization': `Bearer ${token}` } });
  });
});
