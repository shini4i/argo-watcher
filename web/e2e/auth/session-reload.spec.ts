import { expect, test } from '@playwright/test';
import { expectAppLoaded, expectOnApp, KEYCLOAK_ORIGIN, signIn } from '../helpers';

/**
 * Access tokens are held in memory only (never localStorage), so a full page
 * reload drops the session and the app recovers it through the provider's SSO
 * cookie. The bounce is a real cross-document round trip: only a browser shows
 * whether the user lands back where they were or is asked to log in again.
 */
test('a reload recovers the session without showing the login form again', async ({ page }) => {
  await page.goto('/');
  await page.waitForURL(url => url.href.startsWith(KEYCLOAK_ORIGIN));
  await signIn(page);
  await expectAppLoaded(page);

  // The recovery bounce does pass through the provider, so "no login form" has to
  // be observed as a navigation that never reaches the form's submit endpoint;
  // asserting the form is absent once we are back on the app is a tautology.
  const navigations: string[] = [];
  page.on('framenavigated', frame => navigations.push(frame.url()));

  await page.reload();

  await expectAppLoaded(page);
  expect(
    navigations.filter(url => url.includes('/login-actions/authenticate')),
    'recovery must not go through the interactive login form',
  ).toHaveLength(0);
  expectOnApp(page);
});
