import { useEffect, useState } from 'react';
import { httpClient } from '../../data/httpClient';

interface ServerConfigResponse {
  keycloak?: {
    enabled?: boolean;
  };
}

/**
 * Fetches server configuration to determine whether Keycloak authentication
 * is enabled. Returns `null` while the request is in flight so callers can
 * gate privileged actions conservatively (treating "unknown" as "denied")
 * instead of allowing a brief permissive window before the response lands.
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
        if (subscribed) {
          setEnabled(false);
        }
      });

    return () => {
      subscribed = false;
    };
  }, []);

  return enabled;
};
