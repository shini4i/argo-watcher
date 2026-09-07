import { expect, test } from '@playwright/test';
import { KEYCLOAK_ORIGIN, PRIVILEGED_USER, seedTask, signIn, waitForDeployed } from '../helpers';

const OTHER_AUTHOR = 'someone-else@example.com';
const OWN_AUTHOR = 'priv-user@example.com';

/**
 * @description The Mine/Everyone switch exists only for a signed-in reader: it
 * projects the browser's OIDC identity onto the backend's `author` filter.
 * Component tests inject that identity and stub the HTTP client, so only a real
 * sign-in proves the chain — token, identity, `author` query parameter, and the
 * rows the server actually returns.
 */
test('the scope switch narrows the list to the signed-in user, and back', async ({ page, request }) => {
  const mine = await seedTask(request, OWN_AUTHOR);
  const theirs = await seedTask(request, OTHER_AUTHOR);
  await waitForDeployed(request, mine);
  await waitForDeployed(request, theirs);

  await page.goto('/');
  await page.waitForURL(url => url.href.startsWith(KEYCLOAK_ORIGIN));
  await signIn(page, PRIVILEGED_USER.username, PRIVILEGED_USER.password);

  // Mine is the default for a signed-in reader who has never chosen a scope.
  await expect(page.getByRole('tab', { name: 'Mine' })).toHaveAttribute('aria-selected', 'true');
  // The projection is server-side, so the applied filter has to name the address.
  await expect(page).toHaveURL(new RegExp(encodeURIComponent(OWN_AUTHOR)));

  await expect(page.getByText(OWN_AUTHOR).first()).toBeVisible();
  await expect(page.getByText(OTHER_AUTHOR)).toHaveCount(0);

  // The shortcut is advertised only when there is an identity to scope by.
  await expect(page.getByText('m mine')).toBeVisible();

  await page.getByRole('tab', { name: 'Everyone' }).click();

  await expect(page.getByText(OTHER_AUTHOR).first()).toBeVisible();
  await expect(page.getByText(OWN_AUTHOR).first()).toBeVisible();
});
