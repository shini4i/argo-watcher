import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { AuthProvider } from 'react-admin';
import { HttpError } from 'react-admin';
import * as browserUtils from '../shared/utils';
import { getAccessToken, setAccessToken } from './tokenStore';

const keycloakMock = {
  init: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  updateToken: vi.fn(),
  loadUserInfo: vi.fn(),
  token: 'token',
  tokenParsed: {
    sub: 'user-id',
    email: 'user@example.com',
    name: 'User Example',
    groups: ['users', 'admins'],
  },
};

/** Provides a constructor-safe Keycloak stub that always returns the shared mock. */
const mockKeycloakConstructor = vi.fn(function mockKeycloakConstructor() {
  return keycloakMock;
});

vi.mock('keycloak-js', () => ({
  default: mockKeycloakConstructor,
}));

const loadAuthProvider = async (): Promise<AuthProvider & { __testing?: { reset: () => void } }> => {
  const module = await import('./authProvider');
  return module.authProvider as AuthProvider & { __testing?: { reset: () => void } };
};

const resetAuthProvider = async () => {
  const module = await import('./authProvider');
  module.__testing.reset();
};

const mockConfig = (config: unknown) => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(() =>
    Promise.resolve(
      new Response(JSON.stringify(config), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  );
};

/** Ensures a browser-like window object exists when tests require DOM APIs. */
const ensureBrowserWindow = (): Window => {
  const browserWindow = globalThis.window;
  if (!browserWindow) {
    throw new Error('Browser window not available in authProvider tests.');
  }
  return browserWindow;
};

describe('authProvider', () => {
  beforeEach(async () => {
    vi.restoreAllMocks();
    ensureBrowserWindow().localStorage.clear();
    keycloakMock.init.mockReset();
    keycloakMock.login.mockReset();
    keycloakMock.logout.mockReset();
    keycloakMock.updateToken.mockReset();
    keycloakMock.loadUserInfo.mockReset();
    keycloakMock.loadUserInfo.mockResolvedValue({ groups: ['users', 'admins'] });
    await resetAuthProvider();
  });

  it('resolves auth checks when Keycloak is disabled', async () => {
    mockConfig({ keycloak: { enabled: false } });
    const provider = await loadAuthProvider();

    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    await expect(provider.getPermissions()).resolves.toEqual([]);
    const identity = await provider.getIdentity();
    expect(identity.id).toBe('anonymous');
  });

  it('redirects to the Keycloak login page when unauthenticated, without logging out', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: ['admins'],
      },
    });
    keycloakMock.init.mockResolvedValueOnce(false);

    const provider = await loadAuthProvider();

    // checkAuth must NOT reject: a rejection makes react-admin call logout(),
    // which would destroy a still-valid Keycloak session and loop.
    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    expect(keycloakMock.init).toHaveBeenCalledWith(
      expect.objectContaining({ onLoad: 'login-required' }),
    );
    expect(keycloakMock.login).toHaveBeenCalledWith(
      expect.objectContaining({
        redirectUri: `${ensureBrowserWindow().location.origin}/`,
      }),
    );
    expect(keycloakMock.logout).not.toHaveBeenCalled();
  });

  it('never rejects checkAuth (nor logs out) when the login redirect fails', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: [],
      },
    });
    keycloakMock.init.mockResolvedValueOnce(false);
    keycloakMock.login.mockRejectedValueOnce(new Error('redirect blocked'));
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

    const provider = await loadAuthProvider();

    // A login() failure must be swallowed: a rejected checkAuth triggers
    // react-admin's logout(), which would reintroduce the redirect loop.
    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    expect(keycloakMock.logout).not.toHaveBeenCalled();
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining('Failed to initiate the Keycloak login redirect'),
      expect.any(Error),
    );
    warnSpy.mockRestore();
  });

  it('still resolves checkAuth when init throws AND the login redirect rejects', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: [],
      },
    });
    keycloakMock.init.mockRejectedValueOnce(new Error('init failure'));
    keycloakMock.login.mockRejectedValueOnce(new Error('redirect blocked'));
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

    const provider = await loadAuthProvider();

    // Worst case for the loop-prevention invariant: both recovery mechanisms
    // fail in one call. checkAuth must still resolve so react-admin never logs out.
    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    expect(keycloakMock.logout).not.toHaveBeenCalled();
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining('Keycloak initialization failed'),
      expect.any(Error),
    );
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining('Failed to initiate the Keycloak login redirect'),
      expect.any(Error),
    );
    warnSpy.mockRestore();
  });

  it('initializes with login-required rather than a cross-site silent iframe', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: [],
      },
    });
    keycloakMock.init.mockResolvedValueOnce(true);

    const provider = await loadAuthProvider();
    await provider.checkAuth({});

    const initOptions = keycloakMock.init.mock.calls[0]?.[0] ?? {};
    expect(initOptions).toMatchObject({ onLoad: 'login-required', checkLoginIframe: false });
    expect(initOptions).not.toHaveProperty('silentCheckSsoRedirectUri');
    expect(keycloakMock.login).not.toHaveBeenCalled();
  });

  it('returns permissions when Keycloak authenticates successfully', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: ['admins'],
      },
    });
    keycloakMock.init.mockResolvedValueOnce(true);

    const provider = await loadAuthProvider();
    await provider.checkAuth({});
    expect(keycloakMock.init).toHaveBeenCalledWith(
      expect.objectContaining({
        onLoad: 'login-required',
      }),
    );
    const permissions = (await provider.getPermissions()) as { groups: string[]; privilegedGroups: string[] };

    expect(keycloakMock.loadUserInfo).toHaveBeenCalled();
    expect(permissions.groups).toContain('admins');
    expect(permissions.privilegedGroups).toContain('admins');

    const identity = await provider.getIdentity();
    expect(identity.email).toBe('user@example.com');
  });

  it('fetches permissions by authenticating when needed', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: ['admins'],
      },
    });
    keycloakMock.init.mockResolvedValueOnce(true);

    const provider = await loadAuthProvider();
    const permissions = (await provider.getPermissions()) as { groups: string[]; privilegedGroups: string[] };

    expect(keycloakMock.init).toHaveBeenCalledTimes(1);
    expect(keycloakMock.loadUserInfo).toHaveBeenCalledTimes(1);
    expect(permissions.groups).toContain('admins');
    expect(permissions.privilegedGroups).toContain('admins');
  });

  it('revalidates the session at a fixed interval', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: ['admins'],
      },
    });
    keycloakMock.init.mockResolvedValue(true);
    const nowSpy = vi.spyOn(Date, 'now');
    nowSpy.mockReturnValue(0);
    const provider = await loadAuthProvider();
    await provider.checkAuth({});
    await provider.getPermissions();

    keycloakMock.loadUserInfo.mockClear();
    nowSpy.mockReturnValue(120_000);
    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    expect(keycloakMock.loadUserInfo).toHaveBeenCalledTimes(1);
    nowSpy.mockRestore();
  });

  it('forces logout when revalidation fails', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: ['admins'],
      },
    });
    keycloakMock.init.mockResolvedValue(true);
    const nowSpy = vi.spyOn(Date, 'now');
    nowSpy.mockReturnValue(0);
    const provider = await loadAuthProvider();
    await provider.checkAuth({});
    await provider.getPermissions();

    keycloakMock.loadUserInfo.mockClear();
    keycloakMock.loadUserInfo.mockRejectedValueOnce(new Error('disabled'));
    nowSpy.mockReturnValue(120_000);

    await expect(provider.checkAuth({})).rejects.toBeInstanceOf(HttpError);
    nowSpy.mockRestore();
  });

  it('redirects to login (without re-initializing) when Keycloak init fails', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: [],
      },
    });
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const initError = new Error('init failure');
    keycloakMock.init.mockRejectedValueOnce(initError);

    const provider = await loadAuthProvider();

    // A single init attempt (keycloak-js forbids a second init on one instance),
    // then a top-level login redirect — never a rejection that triggers logout.
    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    expect(keycloakMock.init).toHaveBeenCalledTimes(1);
    expect(keycloakMock.login).toHaveBeenCalledTimes(1);
    expect(keycloakMock.logout).not.toHaveBeenCalled();
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining('Keycloak initialization failed'),
      initError,
    );
    warnSpy.mockRestore();
  });

  it('reuses existing tokens instead of re-initializing', async () => {
    ensureBrowserWindow().localStorage.clear();
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: [],
      },
    });
    keycloakMock.init.mockResolvedValue(true);

    const provider = await loadAuthProvider();
    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    keycloakMock.init.mockClear();

    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    expect(keycloakMock.init).not.toHaveBeenCalled();
  });

  it('builds absolute redirect URIs during login', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: [],
      },
    });
    keycloakMock.init.mockResolvedValue(true);

    const provider = await loadAuthProvider();
    await provider.login({ redirectTo: '/history' });

  expect(keycloakMock.login).toHaveBeenCalledWith(
    expect.objectContaining({
      redirectUri: `${ensureBrowserWindow().location.origin}/history`,
    }),
  );
  });

  it('returns cached groups without reloading the user profile', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: ['admins'],
      },
    });
    keycloakMock.init.mockResolvedValue(true);
    const provider = await loadAuthProvider();
    await provider.getPermissions();
    keycloakMock.loadUserInfo.mockReset();
    keycloakMock.loadUserInfo.mockImplementation(() => {
      throw new Error('should not reload');
    });

    const permissions = (await provider.getPermissions()) as { groups: string[] };

    expect(keycloakMock.loadUserInfo).not.toHaveBeenCalled();
    expect(permissions.groups).toContain('admins');
  });

  it('falls back to token groups when user info cannot be loaded', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: ['admins'],
      },
    });
    keycloakMock.init.mockResolvedValue(true);
    keycloakMock.tokenParsed.groups = ['token-only'];
    keycloakMock.loadUserInfo.mockRejectedValueOnce(new Error('userinfo failed'));
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const provider = await loadAuthProvider();

    const permissions = (await provider.getPermissions()) as { groups: string[] };

    expect(permissions.groups).toEqual(['token-only']);
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining('Failed to load user info'),
      expect.any(Error),
    );
    warnSpy.mockRestore();
  });

  it('resolves redirect URIs when the browser origin is unavailable', async () => {
    const module = await import('./authProvider');
    const windowSpy = vi.spyOn(browserUtils, 'getBrowserWindow').mockReturnValue(undefined as unknown as Window);

    expect(module.__testing.resolveRedirectUri()).toBe('/');
    expect(module.__testing.resolveRedirectUri('history')).toBe('/history');

    windowSpy.mockRestore();
  });

  it('propagates configuration endpoint HTTP errors', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: 'boom' }), {
        status: 502,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const provider = await loadAuthProvider();

    await expect(provider.checkAuth({})).rejects.toMatchObject({ status: 502 });
  });

  it('wraps configuration endpoint network failures', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('offline'));
    const provider = await loadAuthProvider();

    await expect(provider.checkAuth({})).rejects.toMatchObject({ status: 0 });
  });

  it('throws when required Keycloak configuration fields are missing', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
      },
    });
    const provider = await loadAuthProvider();

    await expect(provider.checkAuth({})).rejects.toMatchObject({ status: 500 });
  });

  it('warns when the token refresh interval cannot be scheduled', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: [],
      },
    });
    keycloakMock.init.mockResolvedValue(true);
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const windowSpy = vi.spyOn(browserUtils, 'getBrowserWindow').mockReturnValue(undefined as unknown as Window);
    const provider = await loadAuthProvider();

    await provider.checkAuth({});

    expect(warnSpy).toHaveBeenCalledWith(
      '[auth] Unable to schedule token refresh because window is unavailable.',
    );
    windowSpy.mockRestore();
    warnSpy.mockRestore();
  });

  it('clears the cached token when scheduled refresh fails', async () => {
    const module = await import('./authProvider');
    module.__testing.setCachedUserGroups(['alpha']);
    setAccessToken('token');
    const refreshError = new Error('refresh failed');
    keycloakMock.updateToken.mockRejectedValueOnce(refreshError);
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const windowSpy = vi.spyOn(browserUtils, 'getBrowserWindow').mockReturnValue({
      setInterval(handler: () => Promise<void>) {
        handler();
        return 42 as unknown as number;
      },
      clearInterval: vi.fn(),
    } as Window);

    module.__testing.scheduleTokenRefresh(keycloakMock as never);
    await Promise.resolve();

    expect(getAccessToken()).toBeNull();
    expect(module.__testing.getCachedUserGroups()).toBeNull();
    expect(errorSpy).toHaveBeenCalledWith('[auth] Failed to refresh token', refreshError);
    windowSpy.mockRestore();
    errorSpy.mockRestore();
    module.__testing.reset();
  });

  it('short-circuits login when Keycloak is disabled', async () => {
    mockConfig({ keycloak: { enabled: false } });
    const provider = await loadAuthProvider();
    setAccessToken('token');

    await expect(provider.login({ redirectTo: '/history' })).resolves.toBeUndefined();

    expect(keycloakMock.login).not.toHaveBeenCalled();
    expect(getAccessToken()).toBeNull();
  });

  describe('bootstrapAuth', () => {
    it('is a no-op when Keycloak is disabled (preserves keycloak-less mode)', async () => {
      mockConfig({ keycloak: { enabled: false } });
      const module = await import('./authProvider');

      // keycloak-less deployments must render immediately: no init, no redirect.
      await expect(module.bootstrapAuth()).resolves.toBeUndefined();
      expect(keycloakMock.init).not.toHaveBeenCalled();
      expect(keycloakMock.login).not.toHaveBeenCalled();
    });

    it('authenticates eagerly so checkAuth reuses the session without re-initializing', async () => {
      mockConfig({
        keycloak: {
          enabled: true,
          url: 'https://keycloak.example.com',
          realm: 'demo',
          client_id: 'argo',
          privileged_groups: ['admins'],
        },
      });
      keycloakMock.init.mockResolvedValue(true);
      const module = await import('./authProvider');

      // Eager init consumes the login callback before the router strips the URL
      // fragment, persisting the token without ever bouncing to the login page.
      await module.bootstrapAuth();
      expect(keycloakMock.init).toHaveBeenCalledTimes(1);
      expect(getAccessToken()).toBe('token');
      expect(keycloakMock.login).not.toHaveBeenCalled();

      // checkAuth now finds the persisted token and takes the cached-session path,
      // so it never re-enters init. (The init-once guard for the unauthenticated
      // path — where no token is set — is covered by the failed-bootstrap test below.)
      await expect(module.authProvider.checkAuth({})).resolves.toBeUndefined();
      expect(keycloakMock.init).toHaveBeenCalledTimes(1);
    });

    it('redirects to the Keycloak login page when the bootstrap finds no session', async () => {
      mockConfig({
        keycloak: {
          enabled: true,
          url: 'https://keycloak.example.com',
          realm: 'demo',
          client_id: 'argo',
          privileged_groups: [],
        },
      });
      keycloakMock.init.mockResolvedValueOnce(false);
      const module = await import('./authProvider');

      await expect(module.bootstrapAuth()).resolves.toBeUndefined();
      expect(keycloakMock.login).toHaveBeenCalledWith(
        expect.objectContaining({
          redirectUri: `${ensureBrowserWindow().location.origin}/`,
        }),
      );
    });

    it('recovers via login on checkAuth after a failed bootstrap, without re-initializing', async () => {
      mockConfig({
        keycloak: {
          enabled: true,
          url: 'https://keycloak.example.com',
          realm: 'demo',
          client_id: 'argo',
          privileged_groups: [],
        },
      });
      keycloakMock.init.mockRejectedValueOnce(new Error('bootstrap init failure'));
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const module = await import('./authProvider');

      // Bootstrap swallows the init error so rendering is never blocked.
      await expect(module.bootstrapAuth()).resolves.toBeUndefined();
      expect(keycloakMock.init).toHaveBeenCalledTimes(1);

      // The cached rejection means checkAuth must NOT call init again (keycloak-js
      // forbids a second init on one instance); it falls through to a login redirect.
      // login() fires twice in total — once from the failed bootstrap, once from
      // checkAuth — but both merely (re)issue the top-level redirect, never logout.
      await expect(module.authProvider.checkAuth({})).resolves.toBeUndefined();
      expect(keycloakMock.init).toHaveBeenCalledTimes(1);
      expect(keycloakMock.login).toHaveBeenCalledTimes(2);
      expect(keycloakMock.logout).not.toHaveBeenCalled();
      warnSpy.mockRestore();
    });

    it('resolves (never throws) when the config endpoint is unreachable, so render still runs', async () => {
      vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network offline'));
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const module = await import('./authProvider');

      // A down backend must not block the SPA from mounting.
      await expect(module.bootstrapAuth()).resolves.toBeUndefined();
      expect(warnSpy).toHaveBeenCalledWith(
        expect.stringContaining('Eager authentication bootstrap failed'),
        expect.any(Error),
      );
      warnSpy.mockRestore();
    });
  });
});
