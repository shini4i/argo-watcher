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

const SILENT_SSO_STORAGE_KEY = 'argo-watcher:silent-sso-disabled';

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

  it('rejects checkAuth when Keycloak init reports unauthenticated', async () => {
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
    await expect(provider.checkAuth({})).rejects.toBeInstanceOf(HttpError);
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
        silentCheckSsoRedirectUri: expect.stringContaining('silent-check-sso.html'),
        silentCheckSsoFallback: false,
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

  it('falls back to non-silent init when silent SSO fails', async () => {
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
    const silentError = new Error('silent failure');
    keycloakMock.init.mockRejectedValueOnce(silentError);
    keycloakMock.init.mockResolvedValueOnce(true);

    const provider = await loadAuthProvider();
    await expect(provider.checkAuth({})).resolves.toBeUndefined();

    expect(keycloakMock.init).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        silentCheckSsoRedirectUri: expect.stringContaining('silent-check-sso.html'),
      }),
    );
    expect(keycloakMock.init).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        onLoad: 'login-required',
      }),
    );
    expect(keycloakMock.init).toHaveBeenCalledTimes(2);
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining('Silent SSO failed'),
      silentError,
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

  it('remembers silent SSO failures across reloads', async () => {
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
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const silentError = new Error('silent blocked');
    keycloakMock.init.mockRejectedValueOnce(silentError);
    keycloakMock.init.mockResolvedValue(true);

    const provider = await loadAuthProvider();
    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    expect(ensureBrowserWindow().localStorage.getItem(SILENT_SSO_STORAGE_KEY)).toBe('true');
    warnSpy.mockRestore();

    const module = await import('./authProvider');
    module.__testing.reset();
    module.__testing.reloadSilentPreference();
    keycloakMock.init.mockClear();
    keycloakMock.init.mockResolvedValueOnce(true);

    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    expect(keycloakMock.init).toHaveBeenCalledTimes(1);
    expect(keycloakMock.init).toHaveBeenCalledWith(
      expect.objectContaining({
        onLoad: 'login-required',
      }),
    );
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

  it('defaults silent SSO to enabled when localStorage is unavailable', async () => {
    mockConfig({ keycloak: { enabled: false } });
    const module = await import('./authProvider');
    module.__testing.disableSilentSso();
    const windowSpy = vi.spyOn(browserUtils, 'getBrowserWindow').mockReturnValue({} as Window);

    module.__testing.reloadSilentPreference();

    expect(module.__testing.isSilentSsoEnabled()).toBe(true);
    windowSpy.mockRestore();
  });

  it('logs when reading the silent SSO preference fails', async () => {
    mockConfig({ keycloak: { enabled: false } });
    const module = await import('./authProvider');
    module.__testing.disableSilentSso();
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const storageError = new Error('storage-failure');
    const windowSpy = vi.spyOn(browserUtils, 'getBrowserWindow').mockImplementation(() => {
      throw storageError;
    });

    module.__testing.reloadSilentPreference();

    expect(module.__testing.isSilentSsoEnabled()).toBe(true);
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining('Failed to read silent SSO preference'),
      storageError,
    );
    windowSpy.mockRestore();
    warnSpy.mockRestore();
  });

  it('skips persisting silent SSO failures when storage is unavailable', async () => {
    mockConfig({ keycloak: { enabled: false } });
    const module = await import('./authProvider');
    const windowSpy = vi.spyOn(browserUtils, 'getBrowserWindow').mockReturnValue({} as Window);

    module.__testing.disableSilentSso();

    expect(module.__testing.isSilentSsoEnabled()).toBe(false);
    expect(ensureBrowserWindow().localStorage.getItem(SILENT_SSO_STORAGE_KEY)).toBeNull();
    windowSpy.mockRestore();
  });

  it('warns when persisting the silent SSO preference throws', async () => {
    mockConfig({ keycloak: { enabled: false } });
    const module = await import('./authProvider');
    const storageError = new Error('denied');
    const storage = {
      setItem: vi.fn(() => {
        throw storageError;
      }),
      removeItem: vi.fn(() => {
        throw storageError;
      }),
    } as unknown as Storage;
    const windowSpy = vi.spyOn(browserUtils, 'getBrowserWindow').mockReturnValue({
      localStorage: storage,
    } as Window);
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

    module.__testing.disableSilentSso();

    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining('Failed to persist silent SSO preference'),
      storageError,
    );
    windowSpy.mockRestore();
    warnSpy.mockRestore();
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

  it('resolves app URLs when the browser origin is unavailable', async () => {
    const module = await import('./authProvider');
    const windowSpy = vi.spyOn(browserUtils, 'getBrowserWindow').mockReturnValue(undefined as unknown as Window);

    expect(module.__testing.resolveAppUrl('/foo')).toBe('/foo');

    windowSpy.mockRestore();
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

  it('propagates interactive init failures after silent SSO is disabled', async () => {
    mockConfig({
      keycloak: {
        enabled: true,
        url: 'https://keycloak.example.com',
        realm: 'demo',
        client_id: 'argo',
        privileged_groups: [],
      },
    });
    const module = await import('./authProvider');
    module.__testing.disableSilentSso();
    const interactiveError = new Error('interactive failed');
    keycloakMock.init.mockRejectedValueOnce(interactiveError);
    const provider = await loadAuthProvider();

    await expect(provider.checkAuth({})).rejects.toMatchObject({ status: 500 });
  });

  it('short-circuits login when Keycloak is disabled', async () => {
    mockConfig({ keycloak: { enabled: false } });
    const provider = await loadAuthProvider();
    setAccessToken('token');

    await expect(provider.login({ redirectTo: '/history' })).resolves.toBeUndefined();

    expect(keycloakMock.login).not.toHaveBeenCalled();
    expect(getAccessToken()).toBeNull();
  });
});
