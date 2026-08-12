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
    window.sessionStorage.clear();
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

  it('replaces the current history entry when leaving for the provider', async () => {
    mockConfig(enabledConfig());
    userManagerMock.getUser.mockResolvedValue(null);
    const provider = await loadAuthProvider();

    await provider.checkAuth({});

    expect(MockUserManager).toHaveBeenCalledWith(
      expect.objectContaining({ redirectMethod: 'replace' }),
    );
  });

  it('sends the provider a trimmed issuer and client id', async () => {
    mockConfig(
      enabledConfig({ issuer_url: '  https://idp.example.com/realms/demo  ', client_id: ' argo ' }),
    );
    userManagerMock.getUser.mockResolvedValue(null);
    const provider = await loadAuthProvider();

    await provider.checkAuth({});

    // Surrounding whitespace in a deployment value must not become part of the
    // authority or the client id on the wire.
    expect(MockUserManager).toHaveBeenCalledWith(
      expect.objectContaining({
        authority: 'https://idp.example.com/realms/demo',
        client_id: 'argo',
      }),
    );
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

      await expect(module.bootstrapAuth()).resolves.toBeNull();
      expect(MockUserManager).not.toHaveBeenCalled();
      expect(userManagerMock.signinRedirect).not.toHaveBeenCalled();
    });

    it('reports OIDC as enabled before any provider round trip', async () => {
      mockConfig(enabledConfig());
      userManagerMock.getUser.mockResolvedValue(null);
      // Recorded, not asserted, inside the callback: bootstrapAuth catches
      // everything, so a failed expectation in there would be swallowed and the
      // ordering claim would go unchecked.
      let redirectCallsWhenNotified = -1;
      const onOidcEnabled = vi.fn(() => {
        redirectCallsWhenNotified = userManagerMock.signinRedirect.mock.calls.length;
      });
      const module = await import('./authProvider');

      await module.bootstrapAuth({ onOidcEnabled });

      expect(onOidcEnabled).toHaveBeenCalledTimes(1);
      // The caller learns a sign-in is coming before the redirect starts, which is
      // what lets it swap the loading message ahead of the round trip.
      expect(redirectCallsWhenNotified).toBe(0);
      expect(userManagerMock.signinRedirect).toHaveBeenCalledTimes(1);
    });

    it('never reports OIDC as enabled for an auth-less deployment', async () => {
      mockConfig({ oidc: { enabled: false } });
      const onOidcEnabled = vi.fn();
      const module = await import('./authProvider');

      await module.bootstrapAuth({ onOidcEnabled });

      expect(onOidcEnabled).not.toHaveBeenCalled();
    });

    it('reports OIDC as enabled while returning from the provider', async () => {
      mockConfig(enabledConfig());
      window.history.replaceState({}, '', '/?code=abc&state=xyz');
      userManagerMock.signinRedirectCallback.mockResolvedValue(signedInUser());
      // Recorded rather than asserted inside the callback, as above.
      let exchangeCallsWhenNotified = -1;
      const onOidcEnabled = vi.fn(() => {
        exchangeCallsWhenNotified = userManagerMock.signinRedirectCallback.mock.calls.length;
      });
      const module = await import('./authProvider');

      await module.bootstrapAuth({ onOidcEnabled });

      // The code exchange is the longest wait of a sign-in, so it must be labelled
      // as one before the exchange starts — not left under the neutral message.
      expect(onOidcEnabled).toHaveBeenCalledTimes(1);
      expect(exchangeCallsWhenNotified).toBe(0);
      expect(userManagerMock.signinRedirectCallback).toHaveBeenCalledTimes(1);
    });

    it('continues the sign-in when the OIDC-enabled callback throws', async () => {
      mockConfig(enabledConfig());
      userManagerMock.getUser.mockResolvedValue(null);
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const onOidcEnabled = vi.fn(() => {
        throw new Error('render failed');
      });
      const module = await import('./authProvider');

      await module.bootstrapAuth({ onOidcEnabled });

      expect(onOidcEnabled).toHaveBeenCalledTimes(1);
      // A broken loading screen must not cost the user their sign-in.
      expect(userManagerMock.signinRedirect).toHaveBeenCalledTimes(1);
      expect(warnSpy).toHaveBeenCalledWith(
        expect.stringContaining('onOidcEnabled callback threw'),
        expect.any(Error),
      );
      warnSpy.mockRestore();
    });

    it('completes the authorization-code callback and stores the token', async () => {
      mockConfig(enabledConfig());
      window.history.replaceState({}, '', '/?code=abc&state=xyz');
      userManagerMock.signinRedirectCallback.mockResolvedValue(signedInUser());
      const replaceSpy = vi.spyOn(window.history, 'replaceState');
      const module = await import('./authProvider');

      await expect(module.bootstrapAuth()).resolves.toBeNull();
      expect(userManagerMock.signinRedirectCallback).toHaveBeenCalledTimes(1);
      expect(getAccessToken()).toBe('token');
      // The ?code&state query is stripped so a reload does not re-trigger the callback.
      // The empty history state matters too: react-router derives its location key from
      // it, and the `default` key is what tells the task screen it has no in-app history.
      expect(replaceSpy).toHaveBeenCalledWith({}, expect.anything(), module.__testing.appBaseUrl());
      replaceSpy.mockRestore();
    });

    it('redirects to login when the bootstrap finds no session', async () => {
      mockConfig(enabledConfig());
      userManagerMock.getUser.mockResolvedValue(null);
      const module = await import('./authProvider');

      await expect(module.bootstrapAuth()).resolves.toBeNull();
      expect(userManagerMock.signinRedirect).toHaveBeenCalledTimes(1);
    });

    it('consumes a provider error callback instead of looping back to the provider', async () => {
      mockConfig(enabledConfig());
      window.history.replaceState({}, '', '/?error=access_denied&state=xyz');
      userManagerMock.signinRedirectCallback.mockRejectedValue(new Error('access_denied'));
      userManagerMock.getUser.mockResolvedValue(null);
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const module = await import('./authProvider');

      // The error response is recognized as a callback and consumed — not turned
      // into a fresh sign-in. It is reported rather than retried, which is what
      // keeps the caller from mounting an app that would redirect straight back.
      await expect(module.bootstrapAuth()).resolves.not.toBeNull();
      expect(userManagerMock.signinRedirect).not.toHaveBeenCalled();

      // The query is stripped so a reload cannot replay the spent response.
      expect(window.location.search).toBe('');
      warnSpy.mockRestore();
    });

    it('reports a callback that could not be exchanged as a local failure', async () => {
      mockConfig(enabledConfig());
      window.history.replaceState({}, '', '/?code=abc&state=xyz');
      userManagerMock.signinRedirectCallback.mockRejectedValue(
        new Error('No matching state found in storage'),
      );
      userManagerMock.getUser.mockResolvedValue(null);
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const module = await import('./authProvider');

      // The kind picks the title and hint, so a local exchange failure must not
      // reach the screen dressed as a provider rejection.
      const failure = await module.bootstrapAuth();

      expect(failure).toMatchObject({
        kind: 'callback_failed',
        detail: 'No matching state found in storage',
      });
      expect(failure?.code).toBeUndefined();
      warnSpy.mockRestore();
    });

    it('reports the provider error code and description to the caller', async () => {
      mockConfig(enabledConfig());
      window.history.replaceState({}, '', '/?error=invalid_scope&state=xyz');
      userManagerMock.signinRedirectCallback.mockRejectedValue(
        Object.assign(new Error('invalid_scope'), {
          name: 'ErrorResponse',
          error: 'invalid_scope',
          error_description: 'Invalid scopes: groups',
          error_uri: null,
        }),
      );
      userManagerMock.getUser.mockResolvedValue(null);
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const module = await import('./authProvider');

      // The whole point: what the provider said reaches the UI instead of the
      // console, so the user is not left on a silent, session-less app.
      await expect(module.bootstrapAuth()).resolves.toMatchObject({
        kind: 'provider_error',
        code: 'invalid_scope',
        detail: 'Invalid scopes: groups',
      });
      warnSpy.mockRestore();
    });

    it('reports, rather than rejects, when the provider error body is non-conformant', async () => {
      mockConfig(enabledConfig());
      window.history.replaceState({}, '', '/?code=abc&state=xyz');
      // The token-exchange path hands ErrorResponse the provider's parsed JSON
      // as-is, so these fields need not be strings.
      userManagerMock.signinRedirectCallback.mockRejectedValue(
        Object.assign(new Error('invalid_grant'), {
          name: 'ErrorResponse',
          error: 'invalid_grant',
          error_description: { message: 'not a string' },
          error_uri: 42,
        }),
      );
      userManagerMock.getUser.mockResolvedValue(null);
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const module = await import('./authProvider');

      // A rejection here is what reopens the loop: main.tsx's catch renders the
      // app, react-admin re-runs checkAuth, and the same failing exchange repeats
      // on every navigation.
      const failure = await module.bootstrapAuth();

      expect(failure).toMatchObject({ kind: 'provider_error', code: 'invalid_grant' });
      expect(failure?.detail).toBeUndefined();
      expect(failure?.uri).toBeUndefined();
      warnSpy.mockRestore();
    });

    it('reports an unreachable provider, naming the issuer, when the redirect cannot start', async () => {
      mockConfig(enabledConfig());
      userManagerMock.getUser.mockResolvedValue(null);
      userManagerMock.signinRedirect.mockRejectedValueOnce(new TypeError('Failed to fetch'));
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const module = await import('./authProvider');

      const failure = await module.bootstrapAuth();

      expect(failure).toMatchObject({ kind: 'redirect_failed', detail: 'Failed to fetch' });
      // An issuer the server can reach but the browser cannot is the failure this
      // screen exists for, so the hint has to name the URL to try.
      expect(failure?.hint).toContain('https://idp.example.com/realms/demo/.well-known');
      warnSpy.mockRestore();
    });

    it.each([
      { missing: 'client_id', oidc: { enabled: true, issuer_url: 'https://idp/realms/demo' } },
      { missing: 'issuer_url', oidc: { enabled: true, client_id: 'argo' } },
      // Blank counts as absent, or the whitespace reaches discovery and the
      // configuration mistake is reported as an unreachable provider instead.
      {
        missing: 'issuer_url',
        oidc: { enabled: true, issuer_url: '   ', client_id: 'argo' },
      },
      {
        missing: 'client_id',
        oidc: { enabled: true, issuer_url: 'https://idp/realms/demo', client_id: '  ' },
      },
    ])('names $missing when the enabled OIDC block omits it', async ({ missing, oidc }) => {
      mockConfig({ oidc });
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const module = await import('./authProvider');

      const failure = await module.bootstrapAuth();

      expect(failure).toMatchObject({ kind: 'config_incomplete' });
      expect(failure?.detail).toContain(missing);
      expect(userManagerMock.signinRedirect).not.toHaveBeenCalled();
      warnSpy.mockRestore();
    });

    it('still renders the app when a complete config fails to build a UserManager', async () => {
      mockConfig(enabledConfig());
      MockUserManager.mockImplementationOnce(() => {
        throw new Error('constructor blew up');
      });
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const module = await import('./authProvider');

      // Only a configuration the operator can fix becomes a terminal screen.
      // Anything else must still render, with checkAuth re-running the same path.
      await expect(module.bootstrapAuth()).resolves.toBeNull();
      expect(warnSpy).toHaveBeenCalledWith(
        expect.stringContaining('Eager authentication bootstrap failed'),
        expect.any(Error),
      );
      warnSpy.mockRestore();
    });

    it('resolves (never throws) when the config endpoint is unreachable', async () => {
      vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network offline'));
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const module = await import('./authProvider');

      // Reported as no failure on purpose: a backend that is merely restarting
      // must still render the app, not a terminal sign-in error.
      await expect(module.bootstrapAuth()).resolves.toBeNull();
      expect(warnSpy).toHaveBeenCalledWith(
        expect.stringContaining('Eager authentication bootstrap failed'),
        expect.any(Error),
      );
      warnSpy.mockRestore();
    });
  });
});
