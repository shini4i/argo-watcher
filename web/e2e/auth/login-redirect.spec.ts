import { expect, test } from '@playwright/test';
import { expectAppLoaded, expectOnApp, KEYCLOAK_ORIGIN, signIn } from '../helpers';

/**
 * The top-level OIDC redirect: leaving the document for the provider and coming
 * back with an authorization code. jsdom implements no cross-document
 * navigation, so this whole flow is invisible to the unit suite.
 */
test('an unauthenticated visit signs in through the provider and returns to the app', async ({ page }) => {
  await page.goto('/');

  await page.waitForURL(url => url.href.startsWith(KEYCLOAK_ORIGIN));
  await signIn(page);

  expectOnApp(page);
  await expectAppLoaded(page);
  // The callback params must be consumed, not left on the URL where a reload
  // would replay a spent authorization code. The list's own query params (sort,
  // page, filters) are react-admin's and are expected to be there.
  const params = new URL(page.url()).searchParams;
  expect(params.has('code'), 'authorization code must be stripped').toBe(false);
  expect(params.has('state'), 'callback state must be stripped').toBe(false);
});
