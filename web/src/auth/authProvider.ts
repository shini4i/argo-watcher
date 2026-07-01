import Keycloak, { type KeycloakInitOptions, type KeycloakInstance } from 'keycloak-js';
import type { AuthProvider, Identifier } from 'react-admin';
import { HttpError } from 'react-admin';
import { getBrowserWindow } from '../shared/utils';
import { clearAccessToken, getAccessToken, setAccessToken } from './tokenStore';

interface KeycloakConfig {
  enabled: boolean;
  url?: string;
  realm?: string;
  client_id?: string;
  privileged_groups?: string[];
}

interface ServerConfig {
  keycloak: KeycloakConfig;
}

interface Permissions {
  groups: string[];
  privilegedGroups: string[];
}

let serverConfigPromise: Promise<ServerConfig> | null = null;
let serverConfig: ServerConfig | null = null;
let keycloakInstance: KeycloakInstance | null = null;
let initPromise: Promise<boolean> | null = null;
let refreshInterval: number | null = null;
let cachedUserGroups: string[] | null = null;
let userGroupsLoadedFromProfile = false;
let lastSessionValidation = 0;

const SESSION_REVALIDATION_INTERVAL_MS = 60_000;

type GroupSource = 'token' | 'profile';

/**
 * Stores the latest Keycloak group membership along with its data source (token vs userinfo).
 */
const updateUserGroupsCache = (groups?: unknown, source: GroupSource = 'token') => {
  cachedUserGroups = Array.isArray(groups) ? [...groups] : null;
  userGroupsLoadedFromProfile = source === 'profile' && Array.isArray(groups);
};

/**
 * Clears cached user groups and resets session validation timers.
 */
const clearUserGroupsCache = () => {
  cachedUserGroups = null;
  userGroupsLoadedFromProfile = false;
  lastSessionValidation = 0;
};

/** Clears the scheduled token refresh interval when it exists. */
const clearRefreshInterval = () => {
  if (!refreshInterval) {
    return;
  }
  const browserWindow = getBrowserWindow();
  if (browserWindow) {
    browserWindow.clearInterval(refreshInterval);
  }
  refreshInterval = null;
};

interface EnsureGroupsOptions {
  forceReload?: boolean;
  strict?: boolean;
}

/**
 * Ensures Keycloak group membership is available, optionally forcing a reload via userinfo.
 */
const ensureUserGroupsLoaded = async (
  keycloak: KeycloakInstance,
  options: EnsureGroupsOptions = {},
): Promise<string[]> => {
  const needsReload = options.forceReload || !userGroupsLoadedFromProfile || !cachedUserGroups;
  if (!needsReload && Array.isArray(cachedUserGroups)) {
    return cachedUserGroups;
  }
  try {
    const profile = (await keycloak.loadUserInfo()) as { groups?: unknown };
    const profileGroups = Array.isArray(profile?.groups) ? (profile.groups as string[]) : undefined;
    const groups = profileGroups ?? keycloak.tokenParsed?.groups ?? [];
    updateUserGroupsCache(groups, 'profile');
    return cachedUserGroups ?? [];
  } catch (error) {
    if (options.strict) {
      throw error;
    }
    console.warn('[auth] Failed to load user info; falling back to token groups.', error);
    const fallback = keycloak.tokenParsed?.groups ?? [];
    updateUserGroupsCache(fallback, 'token');
    return fallback;
  }
};

/**
 * Periodically revalidates the Keycloak session to capture disabled accounts or group changes.
 */
const ensureSessionValidation = async (keycloak: KeycloakInstance) => {
  const now = Date.now();
  if (now - lastSessionValidation < SESSION_REVALIDATION_INTERVAL_MS) {
    return;
  }

  await ensureUserGroupsLoaded(keycloak, { forceReload: true, strict: true });
  lastSessionValidation = now;
};

/**
 * Resolves the final redirect URI for Keycloak login/logout flows.
 */
const resolveRedirectUri = (redirectTo?: string) => {
  const base = import.meta.env.BASE_URL ?? '/';
  const normalizedBasePath = base.startsWith('/') ? base : `/${base}`;
  const relativeBase = normalizedBasePath.endsWith('/') ? normalizedBasePath : `${normalizedBasePath}/`;
  const browserWindow = getBrowserWindow();
  const origin = browserWindow?.location.origin;
  if (!origin) {
    if (!redirectTo) {
      return relativeBase;
    }
    return `${relativeBase}${redirectTo.replace(/^\//, '')}`;
  }
  const baseUrl = new URL(relativeBase, origin);
  if (!redirectTo) {
    return baseUrl.toString();
  }
  return new URL(redirectTo, baseUrl).toString();
};

/**
 * Fetches the backend configuration once and caches the promise for subsequent calls.
 */
const fetchServerConfig = async (): Promise<ServerConfig> => {
  serverConfigPromise ??= fetch('/api/v1/config', {
    headers: {
      Accept: 'application/json',
    },
  })
    .then(async response => {
      const body = await response.json();
      if (!response.ok) {
        throw new HttpError(body?.error ?? 'Failed to load configuration', response.status, body);
      }
      return body as ServerConfig;
    })
    .catch(error => {
      serverConfigPromise = null;
      if (error instanceof HttpError) {
        throw error;
      }
      throw new HttpError('Failed to load configuration', 0, { cause: error });
    });

  serverConfig = await serverConfigPromise;
  return serverConfig;
};

/**
 * Verifies that the Keycloak config contains the minimum required fields.
 */
const assertKeycloakFields = (config: KeycloakConfig) => {
  if (!config.url || !config.realm || !config.client_id) {
    throw new HttpError('Keycloak configuration is incomplete', 500, config);
  }
};

/**
 * Lazily constructs the singleton Keycloak instance when SSO is enabled server-side.
 */
const ensureKeycloakInstance = async (): Promise<KeycloakInstance | null> => {
  const config = await fetchServerConfig();
  if (!config.keycloak?.enabled) {
    return null;
  }

  assertKeycloakFields(config.keycloak);

  keycloakInstance ??= new Keycloak({
    url: config.keycloak.url!,
    realm: config.keycloak.realm!,
    clientId: config.keycloak.client_id!,
  });

  return keycloakInstance;
};

/**
 * Installs an interval that keeps the Keycloak token fresh and updates cached groups.
 */
const scheduleTokenRefresh = (keycloak: KeycloakInstance) => {
  clearRefreshInterval();
  const browserWindow = getBrowserWindow();
  if (!browserWindow) {
    console.warn('[auth] Unable to schedule token refresh because window is unavailable.');
    return;
  }

  refreshInterval = browserWindow.setInterval(async () => {
    try {
      const refreshed = await keycloak.updateToken(30);
      if (refreshed) {
        setAccessToken(keycloak.token ?? null);
        updateUserGroupsCache(keycloak.tokenParsed?.groups, 'token');
        lastSessionValidation = Date.now();
      }
    } catch (error) {
      console.error('[auth] Failed to refresh token', error);
      clearAccessToken();
      clearUserGroupsCache();
    }
  }, 60_000);
};

/**
 * Creates the Keycloak init options for the login flow.
 *
 * Uses `login-required` so an unauthenticated user is sent to Keycloak through a
 * top-level navigation, which carries the SSO session cookie. The previous
 * `check-sso` flow probed the session inside a cross-site hidden iframe; modern
 * browsers strip third-party cookies from such iframes, so Keycloak answered
 * `login_required` even for users holding a valid session, which triggered an
 * infinite redirect loop between the app and the Keycloak logout endpoint.
 */
const buildInitOptions = (): KeycloakInitOptions => ({
  onLoad: 'login-required',
  checkLoginIframe: false,
  pkceMethod: 'S256',
});

/**
 * Executes keycloak.init with the provided options and wires token persistence.
 */
const runKeycloakInit = async (keycloak: KeycloakInstance, options: KeycloakInitOptions) => {
  const authenticated = await keycloak.init(options);
  if (authenticated) {
    setAccessToken(keycloak.token ?? null);
    updateUserGroupsCache(keycloak.tokenParsed?.groups, 'token');
    lastSessionValidation = Date.now();
    scheduleTokenRefresh(keycloak);
  } else {
    clearAccessToken();
    clearUserGroupsCache();
  }
  return authenticated;
};

/**
 * Runs keycloak.init exactly once for the singleton instance and caches the result.
 *
 * keycloak-js forbids calling init() twice on one instance, and only the first
 * init consumes the login callback (`#state=...&code=...`). The eager bootstrap
 * and the later checkAuth/getPermissions paths both funnel through here, so the
 * callback is processed once and the instance is never re-initialized.
 */
const ensureInitialized = (keycloak: KeycloakInstance): Promise<boolean> => {
  initPromise ??= runKeycloakInit(keycloak, buildInitOptions());
  return initPromise;
};

/**
 * Central authentication path used by the authProvider to ensure the user session is valid.
 *
 * When no local token exists, this initializes Keycloak with `login-required`,
 * which redirects an unauthenticated user to the Keycloak login page through a
 * top-level navigation. It deliberately never rejects for an unauthenticated
 * user: a rejected `checkAuth` makes React-admin call `authProvider.logout()`,
 * which would terminate a still-valid Keycloak session and bounce the browser
 * between the app and the logout endpoint.
 */
const authenticate = async () => {
  const keycloak = await ensureKeycloakInstance();
  if (!keycloak) {
    setAccessToken(null);
    clearUserGroupsCache();
    return true;
  }

  if (getAccessToken()) {
    try {
      await ensureSessionValidation(keycloak);
    } catch (validationError) {
      clearAccessToken();
      clearUserGroupsCache();
      throw new HttpError('Authentication required', 401, { cause: validationError });
    }
    return true;
  }

  let authenticated = false;
  try {
    authenticated = await ensureInitialized(keycloak);
  } catch (error) {
    console.warn('[auth] Keycloak initialization failed; redirecting to the login page.', error);
    clearAccessToken();
    clearUserGroupsCache();
  }

  if (authenticated) {
    return true;
  }

  // Unauthenticated: hand off to Keycloak's login page through a top-level
  // redirect. The navigation carries the SSO session cookie, so a user with an
  // active session is bounced straight back with an authorization code without
  // ever seeing a login form.
  //
  // A login() rejection must not propagate: an unauthenticated checkAuth that
  // rejects makes React-admin call logout(), reintroducing the redirect loop
  // through a different trigger. Swallow it and let the redirect (or the next
  // checkAuth) drive recovery instead.
  try {
    await keycloak.login({ redirectUri: resolveRedirectUri() });
  } catch (loginError) {
    console.warn('[auth] Failed to initiate the Keycloak login redirect.', loginError);
  }
  return true;
};

/**
 * Collects group-based permission metadata for React-admin consumers.
 */
const resolvePermissions = (): Permissions => {
  const config = serverConfig?.keycloak;
  const privilegedGroups = config?.privileged_groups ?? [];
  const groups = cachedUserGroups ?? keycloakInstance?.tokenParsed?.groups ?? [];
  return { groups, privilegedGroups };
};

/**
 * Eagerly initializes authentication before the React tree is mounted.
 *
 * keycloak-js parses the login callback (the `#state=...&code=...` fragment) the
 * first time init() runs. The SPA router performs its default index redirect
 * (`/` -> `/tasks`) the moment it mounts, which strips that fragment. Running
 * init lazily inside checkAuth therefore raced the router and lost the code,
 * leaving Keycloak to redirect to login again -> an endless login loop.
 * Initializing here, before render, consumes the callback while the fragment is
 * still present.
 *
 * Keycloak is OPTIONAL: when SSO is disabled server-side, authenticate() returns
 * without initializing Keycloak or redirecting, so keycloak-less deployments
 * render exactly as before with no added delay. Bootstrap failures are swallowed
 * so rendering is never blocked; checkAuth re-runs the same path on mount.
 */
export const bootstrapAuth = async (): Promise<void> => {
  try {
    await authenticate();
  } catch (error) {
    console.warn('[auth] Eager authentication bootstrap failed; deferring to checkAuth.', error);
  }
};

/**
 * React-admin AuthProvider implementation backed by Keycloak for login, logout, and permissions.
 */
export const authProvider: AuthProvider = {
  async login(params) {
    const keycloak = await ensureKeycloakInstance();
    if (!keycloak) {
      setAccessToken(null);
      clearUserGroupsCache();
      return;
    }

    const redirectUri = resolveRedirectUri(params?.redirectTo);
    await keycloak.login({ redirectUri });
    setAccessToken(keycloak.token ?? null);
  },

  async logout(params) {
    const keycloak = await ensureKeycloakInstance();
    clearRefreshInterval();
    clearAccessToken();
    clearUserGroupsCache();

    if (!keycloak) {
      return;
    }

    const redirectUri = resolveRedirectUri(params?.redirectTo);
    await keycloak.logout({ redirectUri });
  },

  async checkAuth() {
    // authenticate() resolves for a valid or redirecting session and throws only
    // for genuine failures (config errors, a revoked/disabled session). It never
    // resolves "unauthenticated" — an unauthenticated user is redirected to the
    // Keycloak login page instead — so there is no false case to guard here.
    await authenticate();
  },

  async checkError(error) {
    const status = error?.status;
    if (status === 401 || status === 403) {
      clearAccessToken();
      clearUserGroupsCache();
      throw error;
    }
  },

  async getPermissions() {
    const config = await fetchServerConfig();
    if (!config.keycloak?.enabled) {
      return [];
    }

    const keycloak = await ensureKeycloakInstance();
    if (!keycloak) {
      return [];
    }

    await authenticate();

    await ensureUserGroupsLoaded(keycloak);

    const { groups, privilegedGroups } = resolvePermissions();
    return { groups, privilegedGroups };
  },

  async getIdentity(): Promise<{ id: Identifier; fullName?: string; email?: string }> {
    const keycloak = await ensureKeycloakInstance();
    if (!keycloak) {
      return { id: 'anonymous', fullName: 'Anonymous', email: undefined };
    }

    const token = keycloak.tokenParsed ?? {};
    const id = (token.sub as Identifier) ?? 'unknown';
    const fullName = (token.name as string) ?? token.preferred_username ?? undefined;
    const email = (token.email as string) ?? undefined;

    return { id, fullName, email };
  },
};

export const __testing = {
  /** Resets cached Keycloak state for unit tests so each test starts fresh. */
  reset() {
    serverConfigPromise = null;
    serverConfig = null;
    clearRefreshInterval();
    keycloakInstance = null;
    initPromise = null;
    clearAccessToken();
    clearUserGroupsCache();
  },
  /** Allows tests to seed cached user groups without hitting Keycloak. */
  setCachedUserGroups(groups: string[] | null) {
    updateUserGroupsCache(groups ?? undefined);
  },
  /** Provides a snapshot of the cached groups for validation in tests. */
  getCachedUserGroups() {
    return cachedUserGroups ? [...cachedUserGroups] : null;
  },
  /** Exposes redirect resolution for direct verification in tests. */
  resolveRedirectUri,
  /** Exposes the token refresh scheduler for deterministic timer testing. */
  scheduleTokenRefresh,
};
