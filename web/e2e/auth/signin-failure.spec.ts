import { expect, test } from '@playwright/test';
import { expectOnApp, KEYCLOAK_ORIGIN } from '../helpers';

/**
 * The failure this error screen exists for: an issuer the Argo Watcher server can
 * reach but the browser cannot. It is invisible to the unit suite, which mocks the
 * UserManager and so never performs discovery — only a real browser fetching
 * `/.well-known/openid-configuration` can fail this way.
 */
test('an unreachable discovery document stops on the splash instead of redirecting', async ({
  page,
}) => {
  // Blocks only the browser's discovery request; the server keeps validating
  // tokens against the same issuer, which is exactly the asymmetry being tested.
  await page.route('**/.well-known/openid-configuration', route => route.abort());

  const providerNavigations: string[] = [];
  page.on('framenavigated', frame => {
    if (frame.url().startsWith(KEYCLOAK_ORIGIN)) {
      providerNavigations.push(frame.url());
    }
  });

  await page.goto('/');

  const alert = page.getByRole('alert');
  await expect(alert).toBeVisible();
  await expect(alert).toContainText('Could not start the sign-in');
  // The hint has to name the URL to try and the vantage point to try it from,
  // since that is the whole diagnosis the operator cannot get otherwise.
  await expect(alert).toContainText('/.well-known/openid-configuration');
  await expect(alert).toContainText('browser');
  await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();

  // Never bounced to the provider, and the app stayed unmounted: mounting it
  // would re-run checkAuth and retry the same broken discovery on every
  // navigation, which is the loop this screen replaces.
  expectOnApp(page);
  expect(providerNavigations, 'a failed sign-in must not reach the provider').toHaveLength(0);
  await expect(page.getByRole('link', { name: 'Recent' })).toBeHidden();
});
