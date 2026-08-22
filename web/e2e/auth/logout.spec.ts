import { expect, test } from '@playwright/test';
import { expectAppLoaded, KEYCLOAK_ORIGIN, signIn } from '../helpers';

/**
 * Signing out has to end the session at the provider, not just in the browser: the
 * app holds its token in memory only, so a local-only sign-out would be undone by
 * the next silent SSO login. Only a real provider proves the end-session redirect
 * happens and that the app comes back unauthenticated.
 */
test('the account card signs the user out at the provider', async ({ page }) => {
  await page.goto('/');
  await page.waitForURL(url => url.href.startsWith(KEYCLOAK_ORIGIN));
  await signIn(page);
  await expectAppLoaded(page);

  await page.getByRole('button', { name: 'Open configuration drawer' }).click();
  // The account card is the drawer's only menu trigger; its label is the display
  // name the provider issues, which is not this spec's subject.
  await page.locator('[aria-haspopup="menu"]').click();
  await page.getByRole('menuitem', { name: 'Log out' }).click();

  // Whether the provider asks to confirm depends on the id_token_hint it received,
  // so accept the prompt when it appears rather than requiring it.
  await page.waitForURL(url => url.href.startsWith(KEYCLOAK_ORIGIN));
  const confirm = page.getByRole('button', { name: /log ?out/i });
  if (await confirm.isVisible().catch(() => false)) {
    await confirm.click();
  }

  // No session left: the app bounces the returning browser back to the login form.
  await expect(page.locator('#kc-form-login')).toBeVisible({ timeout: 15_000 });
});
