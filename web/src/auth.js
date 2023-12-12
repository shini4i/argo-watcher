import { useEffect, useState, useCallback } from 'react';
import Keycloak from 'keycloak-js';
import { fetchConfig } from './config';

export function useAuth() {
    const [authenticated, setAuthenticated] = useState(null);
    const [profile, setProfile] = useState(null);

    const getUserInfo = useCallback((keycloak) => {
        if (keycloak.authenticated) {
            keycloak.loadUserProfile()
                .then(profile => {
                    console.log('Loaded user profile', profile);
                    setProfile(profile);
                })
                .catch(err => {
                    console.log('Failed to load user profile', err);
                });
        }
    }, []);

    useEffect(() => {
        fetchConfig().then(config => {
            if (config.keycloak_url) {
                const keycloak = new Keycloak({
                    url: config.keycloak_url,
                    realm: config.keycloak_realm,
                    clientId: config.keycloak_client_id,
                });

                keycloak.init({ onLoad: 'login-required' })
                    .then(authenticated => {
                        setAuthenticated(authenticated);
                        getUserInfo(keycloak);
                    })
                    .catch(() => {
                        setAuthenticated(false);
                    });
            } else {
                // keycloak_url is empty, so we just set authenticated to true
                setAuthenticated(true);
            }
        });
    }, [getUserInfo]);

    return { authenticated, profile, getUserInfo };
}
