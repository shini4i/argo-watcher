import { useEffect, useState } from 'react';
import { httpClient } from '../../data/httpClient';

interface ServerConfigResponse {
  keycloak?: {
    enabled?: boolean;
  };
}

/**
 * Fetches server configuration to determine whether Keycloak authentication
 * is enabled. Returns `null` while the request is in flight or when it fails
 * so callers can gate privileged actions conservatively (treating "unknown"
 * as "denied") instead of falling open if the /api/v1/config request errors.
 */
export const useKeycloakEnabled = (): boolean | null => {
  const [enabled, setEnabled] = useState<boolean | null>(null);

  useEffect(() => {
    let subscribed = true;

    httpClient<ServerConfigResponse>('/api/v1/config')
      .then(response => {
        if (subscribed) {
          setEnabled(Boolean(response.data?.keycloak?.enabled));
        }
      })
      .catch(() => {
        // Leave enabled as null on failure — collapsing to false would let
        // ConfigDrawer treat Keycloak as disabled and allow unauthenticated
        // toggling of the deploy lock.
      });

    return () => {
      subscribed = false;
    };
  }, []);

  return enabled;
};
