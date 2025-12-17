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
let refreshInterval: number | null = null;
let cachedUserGroups: string[] | null = null;
let userGroupsLoadedFromProfile = false;
let lastSessionValidation = 0;

const SESSION_REVALIDATION_INTERVAL_MS = 60_000;
const SILENT_SSO_ASSET = 'silent-check-sso.html';

/**
 * LocalStorage flag that records whether silent SSO is safe to execute.
 * Without this cache the app re-attempts the silent flow on every reload,
 * causing infinite login loops when Keycloak is missing the redirect URI.
 */
const SILENT_SSO_DISABLED_KEY = 'argo-watcher:silent-sso-disabled';

/**
 * Reads the persisted silent SSO preference from localStorage.
 * Returns true when the preference indicates silent SSO is disabled.
 */
const readSilentSsoPreference = () => {
  try {
    const storage = getBrowserWindow()?.localStorage;
    if (!storage) {
      return false;
    }
    const disabled = storage.getItem(SILENT_SSO_DISABLED_KEY);
    return disabled === 'true';
  } catch (error) {
    console.warn('[auth] Failed to read silent SSO preference, defaulting to enabled.', error);
    return false;
  }
};

/**
 * Persists the silent SSO preference to localStorage so it survives reloads.
 * When disabled is true the silent flow will be skipped on subsequent visits.
 */
const persistSilentSsoPreference = (disabled: boolean) => {
  try {
    const storage = getBrowserWindow()?.localStorage;
    if (!storage) {
      return;
    }
    if (disabled) {
      storage.setItem(SILENT_SSO_DISABLED_KEY, 'true');
    } else {
      storage.removeItem(SILENT_SSO_DISABLED_KEY);
    }
  } catch (error) {
    console.warn('[auth] Failed to persist silent SSO preference, fallback logic still applies.', error);
  }
};

let silentSsoSupported = !readSilentSsoPreference();

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
 * Builds an absolute URL to a static asset within the SPA using the Vite base URL.
 */
const resolveAppUrl = (path: string) => {
  const base = import.meta.env.BASE_URL ?? '/';
  const normalizedBase = base.endsWith('/') ? base : `${base}/`;
  const normalizedPath = path.startsWith('/') ? path.slice(1) : path;
  const browserWindow = getBrowserWindow();
  const origin = browserWindow?.location.origin;
  if (!origin) {
    return `${normalizedBase}${normalizedPath}`;
  }
  return new URL(normalizedPath, `${origin}${normalizedBase}`).toString();
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
 * Detects whether the current URL contains OAuth authorization code parameters,
 * indicating a redirect back from Keycloak after authentication.
 */
export const hasOAuthCallback = (): boolean => {
  const browserWindow = getBrowserWindow();
  if (!browserWindow) {
    return false;
  }
  const params = new URLSearchParams(browserWindow.location.search);
  return params.has('code') && params.has('state');
};

/**
 * Clears OAuth callback parameters from the URL without triggering a page reload.
 */
const clearOAuthCallbackParams = () => {
  const browserWindow = getBrowserWindow();
  if (!browserWindow) {
    return;
  }
  const url = new URL(browserWindow.location.href);
  url.searchParams.delete('code');
  url.searchParams.delete('state');
  url.searchParams.delete('session_state');
  url.searchParams.delete('iss');
  browserWindow.history.replaceState({}, '', url.toString());
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

type InitMode = 'silent' | 'interactive' | 'callback';

/**
 * Creates Keycloak init options for the silent SSO, interactive, or callback flows.
 */
const buildInitOptions = (mode: InitMode): KeycloakInitOptions => {
  switch (mode) {
    case 'interactive':
      return {
        onLoad: 'login-required',
        checkLoginIframe: false,
        pkceMethod: 'S256',
      };
    case 'callback':
      // No onLoad - processes OAuth callback without auto-redirecting
      return {
        checkLoginIframe: false,
        pkceMethod: 'S256',
      };
    case 'silent':
    default:
      return {
        onLoad: 'check-sso',
        checkLoginIframe: false,
        pkceMethod: 'S256',
        silentCheckSsoRedirectUri: resolveAppUrl(SILENT_SSO_ASSET),
        silentCheckSsoFallback: false,
      };
  }
};

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
 * Central authentication path used by the authProvider to ensure the user session is valid.
 * Does NOT auto-redirect to Keycloak; throws HttpError(401) when not authenticated,
 * allowing React-admin to show the login page.
 */
const authenticate = async () => {
  const keycloak = await ensureKeycloakInstance();
  if (!keycloak) {
    // Keycloak is disabled - allow anonymous access
    setAccessToken(null);
    clearUserGroupsCache();
    return true;
  }

  // If keycloak is already authenticated (e.g., after OAuth callback processed)
  if (keycloak.authenticated && keycloak.token) {
    setAccessToken(keycloak.token);
    scheduleTokenRefresh(keycloak);
    return true;
  }

  // If we have a token, validate the session
  if (getAccessToken()) {
    try {
      await ensureSessionValidation(keycloak);
      return true;
    } catch (validationError) {
      clearAccessToken();
      clearUserGroupsCache();
      // Fall through to try silent SSO
    }
  }

  // Try silent SSO to check for existing session without redirect
  if (silentSsoSupported) {
    try {
      const authenticated = await runKeycloakInit(keycloak, buildInitOptions('silent'));
      if (authenticated) {
        persistSilentSsoPreference(false);
        return true;
      }
    } catch (error) {
      const silentRedirectUri = resolveAppUrl(SILENT_SSO_ASSET);
      console.warn(
        `[auth] Silent SSO failed. Ensure Keycloak client allows ${silentRedirectUri} in redirect URIs.`,
        error,
      );
      silentSsoSupported = false;
      persistSilentSsoPreference(true);
    }
  }

  // NOT authenticated - throw error to trigger login page display
  // DO NOT auto-redirect to Keycloak here!
  clearAccessToken();
  clearUserGroupsCache();
  throw new HttpError('Authentication required', 401);
};

/**
 * Initializes the authentication system. Must be called before React-admin mounts.
 * Handles OAuth callback processing if returning from Keycloak.
 *
 * @returns Object containing keycloakEnabled status
 */
export const initializeAuth = async (): Promise<{ keycloakEnabled: boolean }> => {
  const config = await fetchServerConfig();

  if (!config.keycloak?.enabled) {
    return { keycloakEnabled: false };
  }

  assertKeycloakFields(config.keycloak);

  const keycloak = await ensureKeycloakInstance();
  if (!keycloak) {
    return { keycloakEnabled: false };
  }

  // If we're returning from Keycloak with authorization code, process it
  if (hasOAuthCallback()) {
    try {
      await runKeycloakInit(keycloak, buildInitOptions('callback'));
      // Clear URL params to prevent retry on refresh
      clearOAuthCallbackParams();
    } catch (error) {
      console.error('[auth] Failed to process OAuth callback:', error);
      // Clear URL params to prevent infinite retry loops
      clearOAuthCallbackParams();
      throw new HttpError('Authentication failed', 401, { cause: error });
    }
  }

  return { keycloakEnabled: true };
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
    const authenticated = await authenticate();
    if (!authenticated) {
      throw new HttpError('Authentication required', 401);
    }
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

    const authenticated = await authenticate();
    if (!authenticated) {
      throw new HttpError('Authentication required', 401);
    }

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
    silentSsoSupported = !readSilentSsoPreference();
    clearAccessToken();
    clearUserGroupsCache();
  },
  /** Forces the provider to skip silent SSO attempts, used to simulate iframe failures. */
  disableSilentSso() {
    silentSsoSupported = false;
    persistSilentSsoPreference(true);
  },
  /** Reloads the silent SSO preference from storage to emulate page reloads in tests. */
  reloadSilentPreference() {
    silentSsoSupported = !readSilentSsoPreference();
  },
  /** Returns the current silent SSO flag for white-box assertions. */
  isSilentSsoEnabled() {
    return silentSsoSupported;
  },
  /** Allows tests to seed cached user groups without hitting Keycloak. */
  setCachedUserGroups(groups: string[] | null) {
    updateUserGroupsCache(groups ?? undefined);
  },
  /** Provides a snapshot of the cached groups for validation in tests. */
  getCachedUserGroups() {
    return cachedUserGroups ? [...cachedUserGroups] : null;
  },
  /** Exposes app URL resolution so tests can cover browser-less environments. */
  resolveAppUrl,
  /** Exposes redirect resolution for direct verification in tests. */
  resolveRedirectUri,
  /** Exposes the token refresh scheduler for deterministic timer testing. */
  scheduleTokenRefresh,
  /** Exposes OAuth callback param clearing for tests. */
  clearOAuthCallbackParams,
};
