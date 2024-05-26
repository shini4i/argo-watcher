import { createContext, useEffect, useState } from 'react';
import Keycloak from 'keycloak-js';
import { fetchConfig } from './Data';

type AuthContextType = {
  authenticated: null | boolean;
  email: null | string;
  groups: string[];
  privilegedGroups: string[];
  keycloakToken: null | string;
};

export const AuthContext = createContext<AuthContextType | undefined>(undefined);

/**
 * Custom React Hook for managing Keycloak authentication state.
 *
 * This hook fetches the Keycloak configuration and initializes the Keycloak instance.
 * It then sets the authentication state based on the result of the initialization.
 *
 * @returns An object containing the authentication state and related information.
 */
export function useAuth(): AuthContextType {
  const [authenticated, setAuthenticated] = useState<null | boolean>(null);
  const [email, setEmail] = useState<null | string>(null);
  const [groups, setGroups] = useState<string[]>([]);
  const [privilegedGroups, setPrivilegedGroups] = useState<string[]>([]);
  const [keycloakToken, setKeycloakToken] = useState<null | string>(null);

  /**
   * Refreshes the Keycloak token periodically.
   *
   * @param keycloak - The Keycloak instance.
   * @param config - The Keycloak configuration.
   */
  const refreshToken = (
    keycloak: any,
    config: { keycloak: { token_validation_interval: number } },
  ) => {
    setInterval(() => {
      keycloak.updateToken(20)
        .then((refreshed: boolean) => {
          if (refreshed) {
            console.log('Token refreshed, valid for ' +
              (keycloak.tokenParsed?.exp && keycloak.timeSkew
                ? Math.round(keycloak.tokenParsed.exp + keycloak.timeSkew - new Date().getTime() / 1000)
                : 'Unknown')
              + ' seconds');
            setKeycloakToken(keycloak.token || null);
          }
        }).catch(() => {
        console.error('Failed to refresh token');
      });
    }, config.keycloak.token_validation_interval);
  };

  /**
   * Initializes the Keycloak instance and sets the authentication state.
   *
   * @param config - The Keycloak configuration.
   */
  useEffect(() => {
    fetchConfig().then(config => {
      if (config.keycloak.enabled) {
        const keycloak = new Keycloak({
          url: config.keycloak.url,
          realm: config.keycloak.realm,
          clientId: config.keycloak.client_id,
        });

        keycloak.init({ onLoad: 'login-required' })
          .then(authenticated => {
            setAuthenticated(authenticated);
            if (authenticated) {
              setEmail(keycloak.tokenParsed?.email || null);
              setGroups(keycloak.tokenParsed?.groups || []);
              setPrivilegedGroups(config.keycloak.privileged_groups || []);
              setKeycloakToken(keycloak.token || null);
              refreshToken(keycloak, config);
            }
          })
          .catch(() => {
            setAuthenticated(false);
          });
      } else {
        setAuthenticated(true);
      }
    });
  }, []);

  return { authenticated, email, groups, privilegedGroups, keycloakToken };
}
