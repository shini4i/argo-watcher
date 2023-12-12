import { useEffect, useState } from 'react';
import Keycloak from 'keycloak-js';
import { fetchConfig } from './config';

export function useAuth() {
    const [authenticated, setAuthenticated] = useState(null);
    const [isLoading, setIsLoading] = useState(true); // Add isLoading state

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
                        setIsLoading(false); // Set isLoading to false when authentication is complete
                    })
                    .catch(() => {
                        setAuthenticated(false);
                        setIsLoading(false); // Set isLoading to false when an error occurs
                    });
            } else {
                // keycloak_url is empty, so we just set authenticated to true
                setAuthenticated(true);
                setIsLoading(false); // Set isLoading to false when keycloak_url is empty
            }
        });
    }, []);

    return { authenticated, isLoading }; // Return isLoading along with authenticated
}
