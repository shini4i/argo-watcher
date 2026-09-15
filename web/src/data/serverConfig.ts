import { useEffect, useState } from 'react';
import { HttpError } from 'react-admin';
import { httpClient } from './httpClient';

export interface ServerOidcConfig {
  enabled?: boolean;
  issuer_url?: string;
  client_id?: string;
  privileged_groups?: string[];
  gravatar_fallback?: boolean;
}

/** The subset of GET /api/v1/config the Web UI reads. */
export interface ServerConfig {
  oidc?: ServerOidcConfig;
  state_type?: string;
  argo_cd_url?: string;
  argo_cd_url_alias?: string;
}

let pending: Promise<ServerConfig> | null = null;

/**
 * @returns the server configuration, which is fixed for the life of the server, so every
 * caller shares one in-flight request and its result. A failure clears the cache rather
 * than being remembered, so the next caller retries.
 */
export const fetchServerConfig = async (): Promise<ServerConfig> => {
  pending ??= httpClient<ServerConfig>('/api/v1/config')
    .then(response => {
      // A body-less 200 is a proxy interstitial or the Web UI's own HTML catch-all, not
      // a server with nothing configured. Reading it as {} would start the app as though
      // auth were disabled, leaving every call to 401 with no login path.
      if (!response.data) {
        throw new HttpError('Failed to load configuration', response.status, null);
      }
      return response.data;
    })
    .catch(error => {
      pending = null;
      throw error;
    });

  return pending;
};

/** Drops the cached configuration. Tests need it; nothing in the app does. */
export const resetServerConfigCache = (): void => {
  pending = null;
};

export interface ServerConfigState {
  /** The configuration, or null while it is unknown — in flight or failed. */
  config: ServerConfig | null;
  /** Why the fetch failed, for a caller that reports it; null otherwise. */
  error: Error | null;
}

/**
 * @returns the shared configuration as React state. Config and error are both null while
 * the request is in flight, so a caller gating a privileged action denies until it knows
 * better instead of falling open.
 */
export const useServerConfig = (): ServerConfigState => {
  const [state, setState] = useState<ServerConfigState>({ config: null, error: null });

  useEffect(() => {
    let subscribed = true;

    fetchServerConfig()
      .then(config => {
        if (subscribed) {
          setState({ config, error: null });
        }
      })
      .catch((error: unknown) => {
        if (subscribed) {
          setState({ config: null, error: error instanceof Error ? error : new Error(String(error)) });
        }
      });

    return () => {
      subscribed = false;
    };
  }, []);

  return state;
};
