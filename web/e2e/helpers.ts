import { expect, type APIRequestContext, type Page } from '@playwright/test';

/** Keycloak realm base URL, matching the `integration` compose profile. */
export const KEYCLOAK_ORIGIN = 'http://localhost:8090';

/** Realm users: `priv-user` is in the `privileged` group, `regular-user` is in none. */
export const PRIVILEGED_USER = { username: 'priv-user', password: 'priv-pass' } as const;
export const REGULAR_USER = { username: 'regular-user', password: 'regular-pass' } as const;

/**
 * Application name the mock Argo CD API (cmd/mock) knows about, paired with the
 * image tag it reports as running. A task deploying anything else never reaches a
 * terminal status, so seeds use this pair.
 */
export const MOCK_APP = 'app';
export const MOCK_TAG = 'v0.0.1';

/**
 * `POST /api/v1/tasks` is exempt from OIDC read protection, so this seeds both
 * the authenticated and unauthenticated servers without a credential.
 *
 * @param author - recorded on the task, used by specs to identify their own seed.
 */
export const seedTask = async (request: APIRequestContext, author: string): Promise<string> => {
  const response = await request.post('/api/v1/tasks', {
    data: {
      app: MOCK_APP,
      author,
      project: 'e2e',
      images: [{ image: MOCK_APP, tag: MOCK_TAG }],
    },
  });
  expect(response.status(), 'seeding a task must be accepted').toBe(202);
  const body = (await response.json()) as { id: string };
  expect(body.id, 'seeded task must carry an id').toBeTruthy();
  return body.id;
};

/**
 * Polls a task until it reaches `deployed`, so specs assert against a settled
 * row rather than one still transitioning under them.
 */
export const waitForDeployed = async (request: APIRequestContext, id: string): Promise<void> => {
  await expect
    .poll(
      async () => {
        const response = await request.get(`/api/v1/tasks/${id}`);
        if (!response.ok()) {
          return `http ${response.status()}`;
        }
        const body = (await response.json()) as { status?: string };
        return body.status;
      },
      { message: `task ${id} never reached "deployed"`, timeout: 30_000 },
    )
    .toBe('deployed');
};

/**
 * Resolves once the browser has left the provider's origin — NOT once the app
 * has loaded. Callers must still assert their own render or URL condition.
 *
 * Selectors are Keycloak's stable form element ids rather than visible labels,
 * which change with the login theme and locale.
 *
 * @param page - page currently showing the Keycloak login form.
 */
export const signIn = async (
  page: Page,
  username: string = PRIVILEGED_USER.username,
  password: string = PRIVILEGED_USER.password,
): Promise<void> => {
  await expect(page.locator('#kc-form-login')).toBeVisible();
  await page.locator('#username').fill(username);
  await page.locator('#password').fill(password);
  await page.locator('#kc-login').click();
  // Only that the browser left the provider. Waiting here for the callback query
  // to be consumed would make a spec asserting on that a tautology; callers gate
  // on their own rendered-app assertion instead.
  await page.waitForURL(url => !url.href.startsWith(KEYCLOAK_ORIGIN));
};

/**
 * Every privilege-gated control renders its UNPRIVILEGED form while permissions
 * are still resolving, so a negative assertion made before this settles passes
 * whether the gating works or not. Arm this before signing in, await it after.
 *
 * Keys on the provider's `/protocol/openid-connect/userinfo` response, so a
 * change to how groups are resolved has to update this helper too.
 */
export const awaitPermissionsResolved = (page: Page): Promise<unknown> =>
  page.waitForResponse(
    response =>
      response.request().method() === 'GET' &&
      response.url().includes('/protocol/openid-connect/userinfo') &&
      response.status() === 200,
  );

/** Asserts the browser is no longer parked on the identity provider. */
export const expectOnApp = (page: Page): void => {
  expect(page.url(), 'expected to be on the application origin').not.toContain(KEYCLOAK_ORIGIN);
};

/**
 * Deliberately keys on the top bar rather than the task table: an empty task
 * list renders an empty state with no column headers, so a table-based wait
 * would silently depend on another spec having seeded data first.
 */
export const expectAppLoaded = async (page: Page): Promise<void> => {
  await expect(page.getByRole('link', { name: 'Recent' })).toBeVisible();
};

/**
 * Mints an access token via the direct access grant, for API calls a spec makes
 * outside the browser session.
 */
export const mintToken = async (
  request: APIRequestContext,
  user: { username: string; password: string } = PRIVILEGED_USER,
): Promise<string> => {
  const response = await request.post(`${KEYCLOAK_ORIGIN}/realms/argo-watcher-e2e/protocol/openid-connect/token`, {
    form: {
      grant_type: 'password',
      client_id: 'argo-watcher',
      scope: 'openid',
      username: user.username,
      password: user.password,
    },
  });
  expect(response.ok(), `the provider must issue a token for ${user.username}`).toBeTruthy();
  const body = (await response.json()) as { access_token?: string };
  expect(body.access_token, 'token response must carry an access_token').toBeTruthy();
  return body.access_token!;
};
