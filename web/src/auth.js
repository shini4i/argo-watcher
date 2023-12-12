import { useEffect, useState } from 'react';
import Keycloak from 'keycloak-js';
import { fetchConfig } from './config';

export function useAuth() {
    const [authenticated, setAuthenticated] = useState(null);

    useEffect(() => {
        fetchConfig().then(config => {
            if (config.keycloak_url) {
                const keycloak = new Keycloak({
                    url: config.keycloak_url,
                    realm: config.keycloak_realm,
                    clientId: config.keycloak_client_id,
                });

                keycloak.init({ onLoad: 'check-sso' })
                    .then(authenticated => {
                        setAuthenticated(authenticated);
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

    return authenticated;
}
