import { InMemoryWebStorage, User, UserManager, WebStorageStateStore } from 'oidc-client-ts';
import type { AuthProvider, Identifier } from 'react-admin';
import { HttpError } from 'react-admin';
import { getBrowserWindow } from '../shared/utils';
import type { AuthFailure } from './authFailure';
import {
  describeCallbackError,
  describeIncompleteConfig,
  describeRedirectError,
} from './authFailure';
import { clearAccessToken, setAccessToken } from './tokenStore';

interface OidcConfig {
  enabled: boolean;
  issuer_url?: string;
  client_id?: string;
  privileged_groups?: string[];
  gravatar_fallback?: boolean;
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

/** Reads the `groups` claim from the ID-token profile, not from userinfo. */
const extractProfileGroups = (user: User): string[] => {
  const groups = (user.profile as { groups?: unknown }).groups;
  return Array.isArray(groups) ? [...(groups as string[])] : [];
};

/** Clears cached group membership (e.g. on logout, token renewal, or auth failure). */
const clearUserGroupsCache = () => {
  cachedUserGroups = null;
};

/**
 * Resolves group membership from the provider's userinfo endpoint — the SAME
 * source the backend uses for its privileged-group check — so the UI's button
 * gating always agrees with server-side enforcement. This does not depend on a
 * requested scope or on groups being present in the ID token. Falls back to the
 * ID-token `groups` claim when the userinfo call fails.
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

const ensureGroups = async (manager: UserManager, user: User): Promise<string[]> => {
  if (cachedUserGroups) {
    return cachedUserGroups;
  }
  cachedUserGroups = await loadGroups(manager, user);
  return cachedUserGroups;
};

/**
 * The app's base URL, used as the OIDC redirect and post-logout URIs. With no
 * browser window (SSR/tests) it returns the normalized base path alone.
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
 * Blank counts as missing because the server applies the same rule (it refuses to
 * start when the issuer or client id trims to empty), and because a whitespace
 * issuer would otherwise reach discovery and be reported as an unreachable
 * provider instead of the configuration mistake it is.
 */
const requiredOidcField = (value: unknown): string | undefined => {
  const trimmed = typeof value === 'string' ? value.trim() : '';
  return trimmed || undefined;
};

/** Names the missing fields in configuration-key form, for a user-facing message. */
const missingOidcFields = (config: OidcConfig): string[] =>
  [
    !requiredOidcField(config.issuer_url) && 'issuer_url',
    !requiredOidcField(config.client_id) && 'client_id',
  ].filter((field): field is string => typeof field === 'string');

const assertOidcFields = (config: OidcConfig) => {
  if (missingOidcFields(config).length > 0) {
    throw new HttpError('OIDC configuration is incomplete', 500, config);
  }
};

/**
 * Returns null when OIDC is disabled server-side.
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
      // Normalized, not raw: assertOidcFields has established both are non-blank,
      // and surrounding whitespace would otherwise be sent to the provider.
      authority: requiredOidcField(config.oidc.issuer_url)!,
      client_id: requiredOidcField(config.oidc.client_id)!,
      redirect_uri: redirectUri,
      post_logout_redirect_uri: redirectUri,
      response_type: 'code',
      // Applies to both provider redirects. The default ('assign') would leave the
      // authorize URL in browser history, where a Back press re-authorizes off the
      // SSO cookie and forwards straight back.
      redirectMethod: 'replace',
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

/**
 * Recognizing the `error` form matters as much as `code`: otherwise an error
 * response (denied consent, `login_required`, …) is not consumed and bootstrap
 * starts a fresh sign-in, bouncing between the app and the provider.
 */
const isRedirectCallback = (): boolean => {
  const browserWindow = getBrowserWindow();
  if (!browserWindow) {
    return false;
  }
  const params = new URLSearchParams(browserWindow.location.search);
  return params.has('state') && (params.has('code') || params.has('error'));
};

const replaceUrl = (target: string) => {
  const browserWindow = getBrowserWindow();
  if (browserWindow) {
    browserWindow.history.replaceState({}, browserWindow.document.title, target);
  }
};

/**
 * On success the browser returns to the pre-login path carried in `url_state`.
 * On failure the response is still consumed — the query is stripped so a reload
 * cannot re-trigger it — and the reason is reported for the caller to show
 * instead of retrying.
 *
 * @returns the failure to show the user, or null once a session exists.
 */
const completeSignin = async (manager: UserManager): Promise<AuthFailure | null> => {
  try {
    const user = await manager.signinRedirectCallback();
    setAccessToken(user.access_token);
    clearUserGroupsCache();
    replaceUrl((typeof user.url_state === 'string' && user.url_state) || appBaseUrl());
    return null;
  } catch (error) {
    console.warn('[auth] OIDC sign-in callback returned an error; not retrying automatically.', error);
    replaceUrl(appBaseUrl());
    return describeCallbackError(error);
  }
};

/**
 * Deliberately NEVER rejects for an unauthenticated user: a rejected checkAuth
 * makes React-admin call authProvider.logout(), which would terminate a still-valid
 * SSO session and bounce the browser between the app and the login page. An
 * unauthenticated user is redirected instead, and the call still resolves.
 *
 * @returns the failure to show the user, or null when a session exists or the
 * browser is on its way to the provider.
 */
const ensureAuthenticated = async (manager: UserManager): Promise<AuthFailure | null> => {
  const user = await manager.getUser();
  if (user && !user.expired) {
    setAccessToken(user.access_token);
    return null;
  }

  clearAccessToken();
  clearUserGroupsCache();

  try {
    await manager.signinRedirect({ url_state: currentPath() });
  } catch (error) {
    console.warn('[auth] Failed to initiate the OIDC login redirect.', error);
    return describeRedirectError(error, serverConfig?.oidc?.issuer_url);
  }
  return null;
};

/**
 * Only the one cause an operator can act on becomes a displayable failure: OIDC
 * enabled without the fields it needs. Anything else (a configuration request
 * that did not answer) reports null so the app still renders.
 */
const incompleteConfigFailure = (): AuthFailure | null => {
  const oidc = serverConfig?.oidc;
  if (!oidc?.enabled) {
    return null;
  }
  const missing = missingOidcFields(oidc);
  return missing.length > 0 ? describeIncompleteConfig(missing) : null;
};

interface BootstrapOptions {
  /**
   * Invoked once the server configuration confirms OIDC is enabled, before any
   * provider round trip. Never called for auth-less deployments, which lets the
   * caller keep sign-in-specific UI off screen until a sign-in is actually
   * happening. Runs before the returned promise settles.
   */
  readonly onOidcEnabled?: () => void;
}

/**
 * Must run before the React tree is mounted.
 *
 * The authorization-code callback (`?code=...&state=...`) must be consumed while
 * it is still on the URL: the SPA router performs its default index redirect
 * (`/` -> `/tasks`) the moment it mounts, which would strip those params. Handling
 * the callback here, before render, exchanges the code reliably.
 *
 * OIDC is OPTIONAL: when disabled server-side, this returns without building a
 * UserManager or redirecting, so auth-less deployments render exactly as before.
 *
 * @returns a failure the caller should show instead of mounting the app, or null
 * when the app can render — either because a session exists, because the browser
 * is leaving for the provider, or because OIDC is not in use. A configuration
 * request that merely failed reports null: a restarting backend must still render
 * the app, and checkAuth re-runs the same path on mount.
 */
export const bootstrapAuth = async ({
  onOidcEnabled,
}: BootstrapOptions = {}): Promise<AuthFailure | null> => {
  let manager: UserManager | null = null;

  try {
    manager = await ensureUserManager();
  } catch (error) {
    console.warn('[auth] Eager authentication bootstrap failed; deferring to checkAuth.', error);
    return incompleteConfigFailure();
  }

  if (!manager) {
    setAccessToken(null);
    clearUserGroupsCache();
    return null;
  }

  // Guarded: this callback only drives presentation, so a throw from it must not
  // cancel the sign-in or be reported as an authentication failure.
  try {
    onOidcEnabled?.();
  } catch (error) {
    console.warn('[auth] onOidcEnabled callback threw; continuing with sign-in.', error);
  }

  if (isRedirectCallback()) {
    return completeSignin(manager);
  }

  return ensureAuthenticated(manager);
};

/**
 * SHA-256 Gravatar URL, or undefined without an email or WebCrypto (which is
 * absent outside a secure context). `d=404` so an address with no Gravatar fails
 * the image load and the badge falls back to the initial.
 */
const gravatarUrl = async (email?: string): Promise<string | undefined> => {
  const normalized = email?.trim().toLowerCase();
  const subtle = globalThis.crypto?.subtle;
  if (!normalized || !subtle) {
    return undefined;
  }

  // An optional avatar must never cost the identity it decorates.
  const digest = await subtle
    .digest('SHA-256', new TextEncoder().encode(normalized))
    .catch(() => undefined);
  if (!digest) {
    return undefined;
  }

  const hash = Array.from(new Uint8Array(digest))
    .map(byte => byte.toString(16).padStart(2, '0'))
    .join('');
  return `https://www.gravatar.com/avatar/${hash}?s=80&d=404`;
};

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
    // Throws only for genuine failures (config errors, unreachable backend). An
    // unauthenticated user is redirected rather than rejected — see ensureAuthenticated.
    const manager = await ensureUserManager();
    if (!manager) {
      setAccessToken(null);
      clearUserGroupsCache();
      return;
    }
    // Any failure it reports is left on the console: the app is already mounted by
    // the time checkAuth runs, and tearing a running session down over a failed
    // renewal is the bouncing this provider avoids. Bootstrap is what surfaces a
    // failure on screen, before the app exists.
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
      await ensureAuthenticated(manager);
      return { groups: [], privilegedGroups } satisfies Permissions;
    }

    const groups = await ensureGroups(manager, user);
    return { groups, privilegedGroups } satisfies Permissions;
  },

  async getIdentity(): Promise<{ id: Identifier; fullName?: string; email?: string; avatar?: string }> {
    const manager = await ensureUserManager();
    if (!manager) {
      return { id: 'anonymous', fullName: 'Anonymous', email: undefined, avatar: undefined };
    }

    const user = await manager.getUser();
    const profile = user?.profile ?? {};
    const id = (profile.sub as Identifier) ?? 'unknown';
    // || not ??: a provider that concatenates absent given/family names sends an
    // empty string, which must fall through to the next claim.
    const fullName = (profile.name as string) || (profile.preferred_username as string) || undefined;
    const email = (profile.email as string) || undefined;
    const avatar =
      (profile.picture as string) ||
      (serverConfig?.oidc?.gravatar_fallback ? await gravatarUrl(email) : undefined);

    return { id, fullName, email, avatar };
  },
};

export const __testing = {
  reset() {
    serverConfigPromise = null;
    serverConfig = null;
    userManager = null;
    clearAccessToken();
    clearUserGroupsCache();
  },
  appBaseUrl,
};
