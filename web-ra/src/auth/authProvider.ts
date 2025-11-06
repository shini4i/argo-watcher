import Keycloak from 'keycloak-js';
import type { KeycloakInitOptions, KeycloakInstance } from 'keycloak-js';
import type { AuthProvider, Identifier } from 'react-admin';
import { HttpError } from 'react-admin';
import { clearAccessToken, setAccessToken } from './tokenStore';

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
      }
    } catch (error) {
      console.error('[auth] Failed to refresh token', error);
      clearAccessToken();
    }
  }, 60_000);
};

const authenticate = async () => {
  const keycloak = await ensureKeycloakInstance();
  if (!keycloak) {
    setAccessToken(null);
    return true;
  }

  const options: KeycloakInitOptions = {
    onLoad: 'check-sso',
    checkLoginIframe: false,
    pkceMethod: 'S256',
  };

  try {
    const authenticated = await keycloak.init(options);
    if (authenticated) {
      setAccessToken(keycloak.token ?? null);
      scheduleTokenRefresh(keycloak);
    } else {
      clearAccessToken();
    }
    return authenticated;
  } catch (error) {
    clearAccessToken();
    throw new HttpError('Keycloak initialization failed', 500, { cause: error });
  }
};

const resolvePermissions = (): Permissions => {
  const config = serverConfig?.keycloak;
  const privilegedGroups = config?.privileged_groups ?? [];
  const groups = keycloakInstance?.tokenParsed?.groups ?? [];
  return { groups, privilegedGroups };
};

export const authProvider: AuthProvider = {
  async login(params) {
    const keycloak = await ensureKeycloakInstance();
    if (!keycloak) {
      setAccessToken(null);
      return Promise.resolve();
    }

    const redirectUri = params?.redirectTo ?? window.location.href;
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
      throw error;
    }
  },

  async getPermissions() {
    const config = await fetchServerConfig();
    if (!config.keycloak?.enabled) {
      return [];
    }

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
    clearAccessToken();
  },
};
