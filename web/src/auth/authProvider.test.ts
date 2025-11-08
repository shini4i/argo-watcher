import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { AuthProvider } from 'react-admin';
import { HttpError } from 'react-admin';

vi.mock('keycloak-js', () => {
  return {
    default: vi.fn(() => keycloakMock),
  };
});

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
const getBrowserWindow = (): Window => {
  const browserWindow = globalThis.window;
  if (!browserWindow) {
    throw new Error('Browser window not available in authProvider tests.');
  }
  return browserWindow;
};

describe('authProvider', () => {
  beforeEach(async () => {
    vi.restoreAllMocks();
    getBrowserWindow().localStorage.clear();
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
    getBrowserWindow().localStorage.clear();
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
    getBrowserWindow().localStorage.clear();
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
    expect(getBrowserWindow().localStorage.getItem(SILENT_SSO_STORAGE_KEY)).toBe('true');
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
        redirectUri: `${getBrowserWindow().location.origin}/history`,
      }),
    );
  });
});
