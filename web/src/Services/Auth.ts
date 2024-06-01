import { createContext, useEffect, useState } from 'react';
import Keycloak from 'keycloak-js';

import { fetchConfig } from './Data';

interface AuthContextType {
  authenticated: boolean | null;
  email: string | null;
  groups: string[];
  privilegedGroups: string[];
  keycloakToken: string | null;
}

export const AuthContext = createContext<AuthContextType | undefined>(undefined);

let keycloak: Keycloak | null = null;

export function useAuth() {
  const [authenticated, setAuthenticated] = useState<boolean | null>(null);
  const [email, setEmail] = useState<string | null>(null);
  const [groups, setGroups] = useState<string[]>([]);
  const [privilegedGroups, setPrivilegedGroups] = useState<string[]>([]);
  const [keycloakToken, setKeycloakToken] = useState<string | null>(null);

  const initializeAuth = async () => {
    console.log('initializeAuth triggered');
    try {
      const config = await fetchConfig();

      if (config.keycloak.enabled && !keycloak) {
        keycloak = new Keycloak({
          url: config.keycloak.url,
          realm: config.keycloak.realm,
          clientId: config.keycloak.client_id,
        });

        const authenticated = await keycloak.init({ onLoad: 'login-required' });
        setAuthenticated(authenticated);

        if (authenticated) {
          setEmail(keycloak.tokenParsed?.email || null);
          setGroups(keycloak.tokenParsed?.groups || []);
          setPrivilegedGroups(config.keycloak.privileged_groups);
          setKeycloakToken(keycloak.token || null);
        }
      } else if (!config.keycloak.enabled) {
        setAuthenticated(true);
      }
    } catch (err) {
      console.error('Initialization failed', err);
      setAuthenticated(false);
    }
  };

  useEffect(() => {
    initializeAuth();
  }, []);

  useEffect(() => {
    let intervalId: NodeJS.Timeout | null = null;

    if (authenticated) {
      intervalId = setInterval(() => {
        if (keycloak && keycloak.isTokenExpired(20)) {
          keycloak.updateToken(20)
            .then(refreshed => {
              if (refreshed) {
                console.log('Token refreshed, valid for ' + Math.round((keycloak!.tokenParsed?.exp ?? 0) + (keycloak!.timeSkew ?? 0) - new Date().getTime() / 1000) + ' seconds');
                setKeycloakToken(keycloak!.token || null);
              }
            }).catch((err) => {
            console.error('Failed to refresh token', err);
          });
        }
      }, 60000); // Check token expiration every 60 seconds
    }

    return () => {
      if (intervalId) {
        clearInterval(intervalId);
      }
    };
  }, [authenticated]);

  return { authenticated, email, groups, privilegedGroups, keycloakToken };
}
