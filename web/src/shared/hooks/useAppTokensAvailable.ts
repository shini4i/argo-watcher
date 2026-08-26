import { useEffect, useState } from 'react';
import { httpClient } from '../../data/httpClient';

interface ServerConfigResponse {
  oidc?: {
    enabled?: boolean;
  };
  state_type?: string;
}

/**
 * Reports whether the server exposes the application deploy token endpoints,
 * which need both OIDC and the Postgres state backend.
 *
 * Without this the endpoints are not routes at all, and a request for them falls
 * through to the Web UI's HTML catch-all — which answers 200, so the token list
 * would render empty rather than failing, claiming no tokens exist on a server
 * that cannot hold any. `null` means "not known yet", and callers deny on it.
 */
export const useAppTokensAvailable = (): boolean | null => {
  const [available, setAvailable] = useState<boolean | null>(null);

  useEffect(() => {
    let subscribed = true;

    httpClient<ServerConfigResponse>('/api/v1/config')
      .then(response => {
        if (subscribed) {
          const config = response.data;
          setAvailable(Boolean(config?.oidc?.enabled) && config?.state_type === 'postgres');
        }
      })
      .catch(() => {
        // Left null on failure: collapsing to false would hide the feature on a
        // transient error, and to true would offer a broken page.
      });

    return () => {
      subscribed = false;
    };
  }, []);

  return available;
};
