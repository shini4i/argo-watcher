import { useEffect, useState } from 'react';
import { httpClient } from '../../data/httpClient';

interface ServerConfigResponse {
  keycloak?: {
    enabled?: boolean;
  };
}

/**
 * Fetches server configuration to determine whether Keycloak authentication is enabled.
 */
export const useKeycloakEnabled = () => {
  const [enabled, setEnabled] = useState<boolean>(false);

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
