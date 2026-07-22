import { InMemoryWebStorage, User, UserManager, WebStorageStateStore } from 'oidc-client-ts';
import type { AuthProvider, Identifier } from 'react-admin';
import { HttpError } from 'react-admin';
import { getBrowserWindow } from '../shared/utils';
import { clearAccessToken, setAccessToken } from './tokenStore';

interface OidcConfig {
  enabled: boolean;
  issuer_url?: string;
  client_id?: string;
  privileged_groups?: string[];
}

interface ServerConfig {
  oidc: OidcConfig;
}

interface Permissions {
  groups: string[];
  privilegedGroups: string[];
}

let serverConfigPromise: Promise<ServerConfig> | null = null;
let serverConfig: ServerConfig | null = null;
let userManager: UserManager | null = null;
let cachedUserGroups: string[] | null = null;

/** Reads the `groups` claim from a signed-in user's ID-token profile, defaulting to []. */
const extractProfileGroups = (user: User): string[] => {
  const groups = (user.profile as { groups?: unknown }).groups;
  return Array.isArray(groups) ? [...(groups as string[])] : [];
};

/** Clears cached group membership (e.g. on logout, token renewal, or auth failure). */
const clearUserGroupsCache = () => {
  cachedUserGroups = null;
};

/**
 * Resolves the user's group membership from the provider's userinfo endpoint —
 * the SAME source the backend uses for its privileged-group check — so the UI's
 * button gating always agrees with server-side enforcement. This does not depend
 * on a requested scope or on groups being present in the ID token. Falls back to
 * the ID-token `groups` claim if the userinfo call fails (mirrors the previous
 * keycloak-js loadUserInfo() behaviour and its fallback).
 */
const loadGroups = async (manager: UserManager, user: User): Promise<string[]> => {
  try {
    const userinfoEndpoint = await manager.metadataService.getUserInfoEndpoint();
    const response = await fetch(userinfoEndpoint, {
      headers: {
        Authorization: `Bearer ${user.access_token}`,
        Accept: 'application/json',
      },
    });
    if (response.ok) {
      const info = (await response.json()) as { groups?: unknown };
      if (Array.isArray(info.groups)) {
        return [...(info.groups as string[])];
      }
    }
  } catch (error) {
    console.warn('[auth] Failed to load groups from userinfo; falling back to token claims.', error);
  }
  return extractProfileGroups(user);
};

/** Returns cached groups or resolves them once from userinfo and caches the result. */
const ensureGroups = async (manager: UserManager, user: User): Promise<string[]> => {
  if (cachedUserGroups) {
    return cachedUserGroups;
  }
  cachedUserGroups = await loadGroups(manager, user);
  return cachedUserGroups;
};

/**
 * Resolves the app's base URL (origin + Vite BASE_URL), used as the OIDC
 * redirect and post-logout URIs. When no browser window is available (SSR/tests)
 * it returns the normalized base path alone.
 */
const appBaseUrl = (): string => {
  const base = import.meta.env.BASE_URL ?? '/';
  const normalizedBase = base.startsWith('/') ? base : `/${base}`;
  const relativeBase = normalizedBase.endsWith('/') ? normalizedBase : `${normalizedBase}/`;
  const browserWindow = getBrowserWindow();
  const origin = browserWindow?.location.origin;
  return origin ? new URL(relativeBase, origin).toString() : relativeBase;
};

/** Returns the current in-app path (pathname + search) to restore after login. */
const currentPath = (): string | undefined => {
  const browserWindow = getBrowserWindow();
  if (!browserWindow) {
    return undefined;
  }
  return `${browserWindow.location.pathname}${browserWindow.location.search}`;
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
 * Verifies that the OIDC config contains the minimum fields required to build a
 * UserManager (issuer and client id).
 */
const assertOidcFields = (config: OidcConfig) => {
  if (!config.issuer_url || !config.client_id) {
    throw new HttpError('OIDC configuration is incomplete', 500, config);
  }
};

/**
 * Lazily constructs the singleton UserManager when OIDC is enabled server-side.
 *
 * Tokens are held in an in-memory store (never localStorage) to preserve the
 * original security posture: on a full page reload the in-memory user is gone and
 * the session is silently recovered through the IdP's SSO cookie. The default
 * state store (localStorage) still carries the PKCE state across the login
 * redirect, which is what the authorization-code exchange requires.
 */
const ensureUserManager = async (): Promise<UserManager | null> => {
  const config = await fetchServerConfig();
  if (!config.oidc?.enabled) {
    return null;
  }

  assertOidcFields(config.oidc);

  if (!userManager) {
    const redirectUri = appBaseUrl();
    userManager = new UserManager({
      authority: config.oidc.issuer_url!,
      client_id: config.oidc.client_id!,
      redirect_uri: redirectUri,
      post_logout_redirect_uri: redirectUri,
      response_type: 'code',
      // Request only universally-valid standard scopes. Requesting a `groups`
      // scope would break login on any provider (e.g. a Keycloak client without
      // a registered `groups` client scope) that rejects unknown scopes with
      // invalid_scope. Group membership is read from the ID token `groups` claim
      // (populated by the provider's group mapper, as the OIDC guide requires);
      // the backend independently enforces groups via the userinfo endpoint.
      scope: 'openid profile email',
      automaticSilentRenew: true,
      userStore: new WebStorageStateStore({ store: new InMemoryWebStorage() }),
    });

    // A successful (initial or silently renewed) login persists the fresh token
    // and invalidates cached groups so the next permission check re-reads them
    // from userinfo (membership may have changed across a renewal).
    userManager.events.addUserLoaded(user => {
      setAccessToken(user.access_token);
      clearUserGroupsCache();
    });
    userManager.events.addUserUnloaded(() => {
      clearAccessToken();
      clearUserGroupsCache();
    });
    userManager.events.addSilentRenewError(error => {
      console.error('[auth] Silent token renewal failed', error);
    });
  }

  return userManager;
};

// One-shot marker (per browser session) recording that the last interactive
// sign-in came back as a provider error, so ensureAuthenticated does not
// immediately redirect back to the provider and create a tight redirect loop.
const SIGNIN_ERROR_FLAG = 'argo-watcher:oidc-signin-error';

/**
 * True when the current URL carries an OIDC redirect callback — either a
 * successful authorization code (`code`) or a provider error (`error`), both
 * paired with `state`. Recognizing the error form is essential: otherwise an
 * error response (denied consent, `login_required`, …) is not consumed and
 * bootstrap starts a fresh sign-in, bouncing between the app and the provider.
 */
const isRedirectCallback = (): boolean => {
  const browserWindow = getBrowserWindow();
  if (!browserWindow) {
    return false;
  }
  const params = new URLSearchParams(browserWindow.location.search);
  return params.has('state') && (params.has('code') || params.has('error'));
};

/** Rewrites the URL to `target`, dropping the callback query params. */
const replaceUrl = (target: string) => {
  const browserWindow = getBrowserWindow();
  if (browserWindow) {
    browserWindow.history.replaceState({}, browserWindow.document.title, target);
  }
};

/**
 * Completes an OIDC redirect callback: on success it exchanges the code for
 * tokens and returns to the pre-login path encoded in `url_state`; on a provider
 * error it consumes the response (stripping the query so a reload cannot
 * re-trigger it) and records a one-shot flag so the next auth check does not
 * immediately redirect back — breaking the error → redirect → error loop.
 */
const completeSignin = async (manager: UserManager) => {
  try {
    const user = await manager.signinRedirectCallback();
    setAccessToken(user.access_token);
    clearUserGroupsCache();
    getBrowserWindow()?.sessionStorage.removeItem(SIGNIN_ERROR_FLAG);
    replaceUrl((typeof user.url_state === 'string' && user.url_state) || appBaseUrl());
  } catch (error) {
    console.warn('[auth] OIDC sign-in callback returned an error; not retrying automatically.', error);
    getBrowserWindow()?.sessionStorage.setItem(SIGNIN_ERROR_FLAG, '1');
    replaceUrl(appBaseUrl());
  }
};

/**
 * Ensures the user is authenticated, redirecting to the provider's login page
 * when no valid session exists.
 *
 * It deliberately NEVER rejects for an unauthenticated user: a rejected checkAuth
 * makes React-admin call authProvider.logout(), which would terminate a still-valid
 * SSO session and bounce the browser between the app and the login page. An
 * unauthenticated user is redirected instead, and the call still resolves.
 */
const ensureAuthenticated = async (manager: UserManager): Promise<boolean> => {
  const user = await manager.getUser();
  if (user && !user.expired) {
    setAccessToken(user.access_token);
    return true;
  }

  clearAccessToken();
  clearUserGroupsCache();

  // If the last interactive sign-in just came back as a provider error, do not
  // immediately redirect again (that is the loop). Consume the one-shot flag and
  // resolve unauthenticated for now; a later navigation re-attempts cleanly.
  const browserWindow = getBrowserWindow();
  if (browserWindow?.sessionStorage.getItem(SIGNIN_ERROR_FLAG)) {
    browserWindow.sessionStorage.removeItem(SIGNIN_ERROR_FLAG);
    return true;
  }

  try {
    await manager.signinRedirect({ url_state: currentPath() });
  } catch (error) {
    console.warn('[auth] Failed to initiate the OIDC login redirect.', error);
  }
  return true;
};

/**
 * Eagerly processes authentication before the React tree is mounted.
 *
 * The authorization-code callback (`?code=...&state=...`) must be consumed while
 * it is still on the URL: the SPA router performs its default index redirect
 * (`/` -> `/tasks`) the moment it mounts, which would strip those params. Handling
 * the callback here, before render, exchanges the code reliably.
 *
 * OIDC is OPTIONAL: when disabled server-side, this returns without building a
 * UserManager or redirecting, so auth-less deployments render exactly as before.
 * Bootstrap failures are swallowed so rendering is never blocked; checkAuth re-runs
 * the same path on mount.
 */
export const bootstrapAuth = async (): Promise<void> => {
  try {
    const manager = await ensureUserManager();
    if (!manager) {
      setAccessToken(null);
      clearUserGroupsCache();
      return;
    }

    if (isRedirectCallback()) {
      await completeSignin(manager);
    } else {
      await ensureAuthenticated(manager);
    }
  } catch (error) {
    console.warn('[auth] Eager authentication bootstrap failed; deferring to checkAuth.', error);
  }
};

/**
 * React-admin AuthProvider implementation backed by a generic OIDC provider for
 * login, logout, permissions, and identity.
 */
export const authProvider: AuthProvider = {
  async login(params) {
    const manager = await ensureUserManager();
    if (!manager) {
      setAccessToken(null);
      clearUserGroupsCache();
      return;
    }
    await manager.signinRedirect({ url_state: params?.redirectTo });
  },

  async logout() {
    const manager = await ensureUserManager();
    clearAccessToken();
    clearUserGroupsCache();

    if (!manager) {
      return;
    }

    try {
      await manager.removeUser();
      await manager.signoutRedirect();
    } catch (error) {
      // A provider without an end-session endpoint must not break local logout.
      console.warn('[auth] Provider sign-out redirect failed; cleared local session.', error);
    }
  },

  async checkAuth() {
    // Resolves for a valid or redirecting session; throws only for genuine
    // failures (config errors, unreachable backend). An unauthenticated user is
    // redirected rather than rejected — see ensureAuthenticated.
    const manager = await ensureUserManager();
    if (!manager) {
      setAccessToken(null);
      clearUserGroupsCache();
      return;
    }
    await ensureAuthenticated(manager);
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
    if (!config.oidc?.enabled) {
      return [];
    }

    const manager = await ensureUserManager();
    if (!manager) {
      return [];
    }

    const privilegedGroups = config.oidc.privileged_groups ?? [];
    const user = await manager.getUser();
    if (!user || user.expired) {
      // No usable session — kick off the login redirect and report empty groups.
      await ensureAuthenticated(manager);
      return { groups: [], privilegedGroups } satisfies Permissions;
    }

    const groups = await ensureGroups(manager, user);
    return { groups, privilegedGroups } satisfies Permissions;
  },

  async getIdentity(): Promise<{ id: Identifier; fullName?: string; email?: string }> {
    const manager = await ensureUserManager();
    if (!manager) {
      return { id: 'anonymous', fullName: 'Anonymous', email: undefined };
    }

    const user = await manager.getUser();
    const profile = user?.profile ?? {};
    const id = (profile.sub as Identifier) ?? 'unknown';
    const fullName = (profile.name as string) ?? (profile.preferred_username as string) ?? undefined;
    const email = (profile.email as string) ?? undefined;

    return { id, fullName, email };
  },
};

export const __testing = {
  /** Resets cached OIDC state for unit tests so each test starts fresh. */
  reset() {
    serverConfigPromise = null;
    serverConfig = null;
    userManager = null;
    clearAccessToken();
    clearUserGroupsCache();
  },
  /** Exposes base-URL resolution for direct verification in tests. */
  appBaseUrl,
};
