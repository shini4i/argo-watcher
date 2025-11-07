import Keycloak from 'keycloak-js';
import type { KeycloakInitOptions, KeycloakInstance } from 'keycloak-js';
import type { AuthProvider, Identifier } from 'react-admin';
import { HttpError } from 'react-admin';
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
    const disabled = window.localStorage.getItem(SILENT_SSO_DISABLED_KEY);
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
    if (disabled) {
      window.localStorage.setItem(SILENT_SSO_DISABLED_KEY, 'true');
    } else {
      window.localStorage.removeItem(SILENT_SSO_DISABLED_KEY);
    }
  } catch (error) {
    console.warn('[auth] Failed to persist silent SSO preference, fallback logic still applies.', error);
  }
};

let silentSsoSupported = !readSilentSsoPreference();

type GroupSource = 'token' | 'profile';

const updateUserGroupsCache = (groups?: unknown, source: GroupSource = 'token') => {
  cachedUserGroups = Array.isArray(groups) ? [...groups] : null;
  userGroupsLoadedFromProfile = source === 'profile' && Array.isArray(groups);
};

const clearUserGroupsCache = () => {
  cachedUserGroups = null;
  userGroupsLoadedFromProfile = false;
  lastSessionValidation = 0;
};

interface EnsureGroupsOptions {
  forceReload?: boolean;
  strict?: boolean;
}

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

const ensureSessionValidation = async (keycloak: KeycloakInstance) => {
  const now = Date.now();
  if (now - lastSessionValidation < SESSION_REVALIDATION_INTERVAL_MS) {
    return;
  }

  await ensureUserGroupsLoaded(keycloak, { forceReload: true, strict: true });
  lastSessionValidation = now;
};

const resolveAppUrl = (path: string) => {
  const base = import.meta.env.BASE_URL ?? '/';
  const normalizedBase = base.endsWith('/') ? base : `${base}/`;
  const normalizedPath = path.startsWith('/') ? path.slice(1) : path;
  return new URL(normalizedPath, `${window.location.origin}${normalizedBase}`).toString();
};

const resolveRedirectUri = (redirectTo?: string) => {
  const base = import.meta.env.BASE_URL ?? '/';
  const baseUrl = new URL(base.startsWith('/') ? base : `/${base}`, window.location.origin);
  if (!redirectTo) {
    return baseUrl.toString();
  }
  return new URL(redirectTo, baseUrl).toString();
};

const fetchServerConfig = async (): Promise<ServerConfig> => {
  if (!serverConfigPromise) {
    serverConfigPromise = fetch('/api/v1/config', {
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
  }

  serverConfig = await serverConfigPromise;
  return serverConfig;
};

const assertKeycloakFields = (config: KeycloakConfig) => {
  if (!config.url || !config.realm || !config.client_id) {
    throw new HttpError('Keycloak configuration is incomplete', 500, config);
  }
};

const ensureKeycloakInstance = async (): Promise<KeycloakInstance | null> => {
  const config = await fetchServerConfig();
  if (!config.keycloak?.enabled) {
    return null;
  }

  assertKeycloakFields(config.keycloak);

  if (!keycloakInstance) {
    keycloakInstance = new Keycloak({
      url: config.keycloak.url!,
      realm: config.keycloak.realm!,
      clientId: config.keycloak.client_id!,
    });
  }

  return keycloakInstance;
};

const scheduleTokenRefresh = (keycloak: KeycloakInstance) => {
  if (refreshInterval) {
    window.clearInterval(refreshInterval);
  }

  refreshInterval = window.setInterval(async () => {
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

type InitMode = 'silent' | 'interactive';

const buildInitOptions = (mode: InitMode): KeycloakInitOptions => {
  switch (mode) {
    case 'interactive':
      return {
        onLoad: 'login-required',
        checkLoginIframe: false,
        pkceMethod: 'S256',
      };
    case 'silent':
    default:
      return {
        onLoad: 'check-sso',
        checkLoginIframe: false,
        pkceMethod: 'S256',
        silentCheckSsoRedirectUri: resolveAppUrl('silent-check-sso.html'),
        silentCheckSsoFallback: false,
      };
  }
};

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

  if (silentSsoSupported) {
    try {
      const authenticated = await runKeycloakInit(keycloak, buildInitOptions('silent'));
      persistSilentSsoPreference(false);
      return authenticated;
    } catch (error) {
      console.warn(
        '[auth] Silent SSO failed, falling back to explicit login flow. Ensure the Keycloak client allows /silent-check-sso.html in redirect URIs.',
        error,
      );
      silentSsoSupported = false;
      persistSilentSsoPreference(true);
      clearAccessToken();
      clearUserGroupsCache();
    }
  }

  try {
    return await runKeycloakInit(keycloak, buildInitOptions('interactive'));
  } catch (fallbackError) {
    clearAccessToken();
    clearUserGroupsCache();
    throw new HttpError('Keycloak initialization failed', 500, { cause: fallbackError });
  }
};

const resolvePermissions = (): Permissions => {
  const config = serverConfig?.keycloak;
  const privilegedGroups = config?.privileged_groups ?? [];
  const groups = cachedUserGroups ?? keycloakInstance?.tokenParsed?.groups ?? [];
  return { groups, privilegedGroups };
};

export const authProvider: AuthProvider = {
  async login(params) {
    const keycloak = await ensureKeycloakInstance();
    if (!keycloak) {
      setAccessToken(null);
      clearUserGroupsCache();
      return Promise.resolve();
    }

    const redirectUri = resolveRedirectUri(params?.redirectTo);
    await keycloak.login({ redirectUri });
    setAccessToken(keycloak.token ?? null);
  },

  async logout(params) {
    const keycloak = await ensureKeycloakInstance();
    if (refreshInterval) {
      window.clearInterval(refreshInterval);
      refreshInterval = null;
    }
    clearAccessToken();
    clearUserGroupsCache();

    if (!keycloak) {
      return Promise.resolve();
    }

    const redirectUri = params?.redirectTo ?? window.location.origin;
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
  reset() {
    serverConfigPromise = null;
    serverConfig = null;
    if (refreshInterval) {
      window.clearInterval(refreshInterval);
      refreshInterval = null;
    }
    keycloakInstance = null;
    silentSsoSupported = !readSilentSsoPreference();
    clearAccessToken();
    clearUserGroupsCache();
  },
  disableSilentSso() {
    silentSsoSupported = false;
    persistSilentSsoPreference(true);
  },
  reloadSilentPreference() {
    silentSsoSupported = !readSilentSsoPreference();
  },
};
