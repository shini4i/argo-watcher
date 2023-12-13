import {createContext, useEffect, useState} from 'react';
import Keycloak from 'keycloak-js';
import {fetchConfig} from './config';

export const AuthContext = createContext(undefined);

export function useAuth() {
    const [authenticated, setAuthenticated] = useState(null);
    const [email, setEmail] = useState(null);
    const [groups, setGroups] = useState([]);
    const [privilegedGroups, setPrivilegedGroups] = useState([]);

    useEffect(() => {
        fetchConfig().then(config => {
            if (config.keycloak_url) {
                const keycloak = new Keycloak({
                    url: config.keycloak_url,
                    realm: config.keycloak_realm,
                    clientId: config.keycloak_client_id,
                });

                keycloak.init({onLoad: 'login-required'})
                    .then(authenticated => {
                        setAuthenticated(authenticated);
                        if (authenticated) {
                            setEmail(keycloak.tokenParsed.email);
                            setGroups(keycloak.tokenParsed.groups);
                            setPrivilegedGroups(config.keycloak_privileged_groups)
                        }
                    })
                    .catch(() => {
                        setAuthenticated(false);
                    });
            } else {
                // keycloak_url is empty, so we just set authenticated to true
                setAuthenticated(true);
            }
        });
    }, []);

    return {authenticated, email, groups, privilegedGroups};
}
