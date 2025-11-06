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
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(JSON.stringify(config), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }),
  );
};

describe('authProvider', () => {
  beforeEach(async () => {
    vi.restoreAllMocks();
    keycloakMock.init.mockReset();
    keycloakMock.login.mockReset();
    keycloakMock.logout.mockReset();
    keycloakMock.updateToken.mockReset();
    await resetAuthProvider();
  });

  it('resolves auth checks when Keycloak is disabled', async () => {
    mockConfig({ keycloak: { enabled: false } });
    const provider = await loadAuthProvider();

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
    const permissions = (await provider.getPermissions()) as { groups: string[]; privilegedGroups: string[] };

    expect(permissions.groups).toContain('admins');
    expect(permissions.privilegedGroups).toContain('admins');

    const identity = await provider.getIdentity();
    expect(identity.email).toBe('user@example.com');
  });
});
