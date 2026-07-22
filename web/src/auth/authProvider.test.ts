import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { AuthProvider } from 'react-admin';
import { getAccessToken, setAccessToken } from './tokenStore';

// Shared mock UserManager instance the module under test receives from every
// `new UserManager(...)` call, so tests can drive its behaviour.
const userManagerEvents = {
  addUserLoaded: vi.fn(),
  addUserUnloaded: vi.fn(),
  addSilentRenewError: vi.fn(),
  addAccessTokenExpiring: vi.fn(),
};

const userManagerMock = {
  signinRedirect: vi.fn(),
  signinRedirectCallback: vi.fn(),
  signinSilent: vi.fn(),
  getUser: vi.fn(),
  removeUser: vi.fn(),
  signoutRedirect: vi.fn(),
  events: userManagerEvents,
  metadataService: {
    getUserInfoEndpoint: vi.fn(),
  },
};

const MockUserManager = vi.fn(function MockUserManager() {
  return userManagerMock;
});

vi.mock('oidc-client-ts', () => ({
  UserManager: MockUserManager,
  WebStorageStateStore: class {
    constructor(_args: unknown) {}
  },
  InMemoryWebStorage: class {},
  User: class {},
}));

const loadAuthProvider = async (): Promise<AuthProvider & { __testing: { reset: () => void } }> => {
  const module = await import('./authProvider');
  return module.authProvider as AuthProvider & { __testing: { reset: () => void } };
};

const resetAuthProvider = async () => {
  const module = await import('./authProvider');
  module.__testing.reset();
};

// Routes the two fetches the module makes: /api/v1/config and the provider's
// userinfo endpoint (from which group membership is read, like the backend does).
const mockConfig = (config: unknown, groups: string[] = ['users', 'admins']) => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString();
    const body = url.includes('userinfo') ? { groups } : config;
    return Promise.resolve(
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
  });
};

const enabledConfig = (overrides: Record<string, unknown> = {}) => ({
  oidc: {
    enabled: true,
    issuer_url: 'https://idp.example.com/realms/demo',
    client_id: 'argo',
    privileged_groups: ['admins'],
    ...overrides,
  },
});

const signedInUser = (overrides: Record<string, unknown> = {}) => ({
  access_token: 'token',
  expired: false,
  url_state: undefined,
  profile: {
    sub: 'user-id',
    email: 'user@example.com',
    name: 'User Example',
    preferred_username: 'user',
    groups: ['users', 'admins'],
    ...overrides,
  },
});

describe('authProvider', () => {
  beforeEach(async () => {
    vi.restoreAllMocks();
    window.localStorage.clear();
    // Reset the URL so callback-detection starts clean for every test.
    window.history.replaceState({}, '', '/');
    MockUserManager.mockClear();
    userManagerMock.signinRedirect.mockReset();
    userManagerMock.signinRedirectCallback.mockReset();
    userManagerMock.signinSilent.mockReset();
    userManagerMock.getUser.mockReset();
    userManagerMock.removeUser.mockReset();
    userManagerMock.signoutRedirect.mockReset();
    userManagerMock.metadataService.getUserInfoEndpoint.mockReset();
    userManagerMock.signinRedirect.mockResolvedValue(undefined);
    userManagerMock.removeUser.mockResolvedValue(undefined);
    userManagerMock.signoutRedirect.mockResolvedValue(undefined);
    userManagerMock.metadataService.getUserInfoEndpoint.mockResolvedValue('https://idp.example.com/userinfo');
    await resetAuthProvider();
  });

  it('resolves auth checks and reports anonymous when OIDC is disabled', async () => {
    mockConfig({ oidc: { enabled: false } });
    const provider = await loadAuthProvider();

    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    await expect(provider.getPermissions({})).resolves.toEqual([]);
    const identity = await provider.getIdentity!();
    expect(identity.id).toBe('anonymous');
    expect(MockUserManager).not.toHaveBeenCalled();
  });

  it('redirects to the provider login when unauthenticated, without rejecting checkAuth', async () => {
    mockConfig(enabledConfig());
    userManagerMock.getUser.mockResolvedValue(null);
    const provider = await loadAuthProvider();

    // checkAuth must NOT reject: a rejection makes react-admin call logout(),
    // which would destroy a still-valid session and loop.
    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    expect(userManagerMock.signinRedirect).toHaveBeenCalledTimes(1);
    expect(userManagerMock.signoutRedirect).not.toHaveBeenCalled();
  });

  it('never rejects checkAuth when the login redirect fails', async () => {
    mockConfig(enabledConfig());
    userManagerMock.getUser.mockResolvedValue(null);
    userManagerMock.signinRedirect.mockRejectedValueOnce(new Error('redirect blocked'));
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const provider = await loadAuthProvider();

    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    expect(userManagerMock.signoutRedirect).not.toHaveBeenCalled();
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining('Failed to initiate the OIDC login redirect'),
      expect.any(Error),
    );
    warnSpy.mockRestore();
  });

  it('accepts an existing valid session and stores the token', async () => {
    mockConfig(enabledConfig());
    userManagerMock.getUser.mockResolvedValue(signedInUser());
    const provider = await loadAuthProvider();

    await expect(provider.checkAuth({})).resolves.toBeUndefined();
    expect(userManagerMock.signinRedirect).not.toHaveBeenCalled();
    expect(getAccessToken()).toBe('token');
  });

  it('returns group-based permissions for an authenticated user', async () => {
    mockConfig(enabledConfig());
    userManagerMock.getUser.mockResolvedValue(signedInUser());
    const provider = await loadAuthProvider();

    const permissions = (await provider.getPermissions({})) as { groups: string[]; privilegedGroups: string[] };
    expect(permissions.groups).toContain('admins');
    expect(permissions.privilegedGroups).toContain('admins');

    const identity = await provider.getIdentity!();
    expect(identity.email).toBe('user@example.com');
    expect(identity.id).toBe('user-id');
  });

  it('reads groups from userinfo (the source the backend enforces on), not the ID token', async () => {
    // ID token carries stale/no groups; userinfo is authoritative.
    mockConfig(enabledConfig(), ['admins']);
    userManagerMock.getUser.mockResolvedValue(signedInUser({ groups: [] }));
    const provider = await loadAuthProvider();

    const permissions = (await provider.getPermissions({})) as { groups: string[] };
    expect(permissions.groups).toEqual(['admins']);
    expect(userManagerMock.metadataService.getUserInfoEndpoint).toHaveBeenCalled();
  });

  it('falls back to ID-token groups when the userinfo request fails', async () => {
    mockConfig(enabledConfig());
    userManagerMock.getUser.mockResolvedValue(signedInUser({ groups: ['token-only'] }));
    // Make only the userinfo fetch fail; config fetch still succeeds.
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.includes('userinfo')) {
        return Promise.reject(new Error('userinfo down'));
      }
      return Promise.resolve(
        new Response(JSON.stringify(enabledConfig()), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    });
    const provider = await loadAuthProvider();

    const permissions = (await provider.getPermissions({})) as { groups: string[] };
    expect(permissions.groups).toEqual(['token-only']);
    warnSpy.mockRestore();
  });

  it('reports empty groups and redirects when getPermissions finds no session', async () => {
    mockConfig(enabledConfig());
    userManagerMock.getUser.mockResolvedValue(null);
    const provider = await loadAuthProvider();

    const permissions = (await provider.getPermissions({})) as { groups: string[]; privilegedGroups: string[] };
    expect(permissions.groups).toEqual([]);
    expect(permissions.privilegedGroups).toContain('admins');
    expect(userManagerMock.signinRedirect).toHaveBeenCalled();
  });

  it('builds a signin redirect carrying the requested return path on login', async () => {
    mockConfig(enabledConfig());
    const provider = await loadAuthProvider();

    await provider.login({ redirectTo: '/history' });
    expect(userManagerMock.signinRedirect).toHaveBeenCalledWith(
      expect.objectContaining({ url_state: '/history' }),
    );
  });

  it('clears the token and redirects to end-session on logout', async () => {
    mockConfig(enabledConfig());
    setAccessToken('token');
    const provider = await loadAuthProvider();

    await provider.logout({});
    expect(getAccessToken()).toBeNull();
    expect(userManagerMock.removeUser).toHaveBeenCalled();
    expect(userManagerMock.signoutRedirect).toHaveBeenCalled();
  });

  it('short-circuits login when OIDC is disabled', async () => {
    mockConfig({ oidc: { enabled: false } });
    setAccessToken('token');
    const provider = await loadAuthProvider();

    await expect(provider.login({ redirectTo: '/history' })).resolves.toBeUndefined();
    expect(userManagerMock.signinRedirect).not.toHaveBeenCalled();
    expect(getAccessToken()).toBeNull();
  });

  it('clears local session on a 401 error', async () => {
    mockConfig(enabledConfig());
    setAccessToken('token');
    const provider = await loadAuthProvider();

    await expect(provider.checkError({ status: 401 })).rejects.toMatchObject({ status: 401 });
    expect(getAccessToken()).toBeNull();
  });

  it('throws a 500 when required OIDC configuration fields are missing', async () => {
    mockConfig({ oidc: { enabled: true, issuer_url: 'https://idp.example.com/realms/demo' } });
    const provider = await loadAuthProvider();

    await expect(provider.checkAuth({})).rejects.toMatchObject({ status: 500 });
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

  describe('bootstrapAuth', () => {
    it('is a no-op when OIDC is disabled (preserves auth-less mode)', async () => {
      mockConfig({ oidc: { enabled: false } });
      const module = await import('./authProvider');

      await expect(module.bootstrapAuth()).resolves.toBeUndefined();
      expect(MockUserManager).not.toHaveBeenCalled();
      expect(userManagerMock.signinRedirect).not.toHaveBeenCalled();
    });

    it('completes the authorization-code callback and stores the token', async () => {
      mockConfig(enabledConfig());
      window.history.replaceState({}, '', '/?code=abc&state=xyz');
      userManagerMock.signinRedirectCallback.mockResolvedValue(signedInUser());
      const replaceSpy = vi.spyOn(window.history, 'replaceState');
      const module = await import('./authProvider');

      await expect(module.bootstrapAuth()).resolves.toBeUndefined();
      expect(userManagerMock.signinRedirectCallback).toHaveBeenCalledTimes(1);
      expect(getAccessToken()).toBe('token');
      // The ?code&state query is stripped so a reload does not re-trigger the callback.
      expect(replaceSpy).toHaveBeenCalled();
      replaceSpy.mockRestore();
    });

    it('redirects to login when the bootstrap finds no session', async () => {
      mockConfig(enabledConfig());
      userManagerMock.getUser.mockResolvedValue(null);
      const module = await import('./authProvider');

      await expect(module.bootstrapAuth()).resolves.toBeUndefined();
      expect(userManagerMock.signinRedirect).toHaveBeenCalledTimes(1);
    });

    it('resolves (never throws) when the config endpoint is unreachable', async () => {
      vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network offline'));
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const module = await import('./authProvider');

      await expect(module.bootstrapAuth()).resolves.toBeUndefined();
      expect(warnSpy).toHaveBeenCalledWith(
        expect.stringContaining('Eager authentication bootstrap failed'),
        expect.any(Error),
      );
      warnSpy.mockRestore();
    });
  });
});
