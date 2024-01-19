import {createContext, useEffect, useState} from 'react';
import Keycloak from 'keycloak-js';
import {fetchConfig} from './config';

export const AuthContext = createContext(undefined);

export function useAuth() {
    const [authenticated, setAuthenticated] = useState(null);
    const [email, setEmail] = useState(null);
    const [groups, setGroups] = useState([]);
    const [privilegedGroups, setPrivilegedGroups] = useState([]);
    const [keycloakToken, setKeycloakToken] = useState(null);

    useEffect(() => {
        fetchConfig().then(config => {
            if (config.keycloak.url) {
                const keycloak = new Keycloak({
                    url: config.keycloak.url,
                    realm: config.keycloak.realm,
                    clientId: config.keycloak.client_id,
                });

                keycloak.init({ onLoad: 'login-required' })
                    .then(authenticated => {
                        setAuthenticated(authenticated);
                        if (authenticated) {
                            setEmail(keycloak.tokenParsed.email);
                            setGroups(keycloak.tokenParsed.groups);
                            setPrivilegedGroups(config.keycloak.privileged_groups);
                            setKeycloakToken(keycloak.token);

                            setInterval(() => {
                                keycloak.updateToken(20)
                                    .then(refreshed => {
                                        if (refreshed) {
                                            console.log('Token refreshed, valid for ' + Math.round(keycloak.tokenParsed.exp + keycloak.timeSkew - new Date().getTime() / 1000) + ' seconds');
                                            // we need to set the token again here to handle cases
                                            // when the UI was open for a long time
                                            setKeycloakToken(keycloak.token);
                                        }
                                    }).catch(() => {
                                    console.error('Failed to refresh token');
                                });
                            }, config.keycloak.token_validation_interval);
                        }
                    })
                    .catch(() => {
                        setAuthenticated(false);
                    });
            } else {
                // if keycloak_url is not set, we are not using any authentication
                // hence we set authenticated to true by default
                setAuthenticated(true);
            }
        });
    }, []);

    return { authenticated, email, groups, privilegedGroups, keycloakToken };
}
