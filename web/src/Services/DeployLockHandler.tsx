import React, { createContext, useContext, useEffect, useState, ReactNode } from 'react';

/**
 * Fetches the deploy lock status from the server.
 *
 * @returns A promise that resolves to a boolean value representing the deploy lock status.
 */
export async function fetchDeployLock(): Promise<boolean> {
    const response = await fetch('/api/v1/deploy-lock');
    return await response.json();
}

/**
 * Releases the deploy lock on the server.
 *
 * @param {string | null} keycloakToken - The token used for authentication with the Keycloak server.
 * @returns {Promise<void>} - A promise that resolves when the deploy lock is successfully released.
 * @throws {Error} - If the server returns a status code other than 200.
 */
export async function releaseDeployLock(keycloakToken: string | null): Promise<void> {
    let headers: Record<string, string> = {
        'Content-Type': 'application/json',
    };

    if (keycloakToken !== null) {
        headers['Keycloak-Authorization'] = keycloakToken;
    }

    const response = await fetch('/api/v1/deploy-lock', {
        method: 'DELETE',
        headers: headers,
    });

    if (response.status !== 200) {
        throw new Error(`Error: ${response.status}`);
    }
}

/**
 * Sets the deploy lock on the server.
 *
 * @param {string | null} keycloakToken - The token used for authentication with the Keycloak server.
 * @returns {Promise<void>} - A promise that resolves when the deploy lock is successfully set.
 * @throws {Error} - If the server returns a status code other than 200.
 */
export async function setDeployLock(keycloakToken: string | null = null): Promise<void> {
    let headers: Record<string, string> = {
        'Content-Type': 'application/json',
    };

    if (keycloakToken !== null) {
        headers['Keycloak-Authorization'] = keycloakToken;
    }

    const response = await fetch('/api/v1/deploy-lock', {
        method: 'POST',
        headers: headers,
    });

    if (response.status !== 200) {
        throw new Error(`Error: ${response.status}`);
    }
}

export const DeployLockContext = createContext<boolean>(false);

interface DeployLockProviderProps {
    readonly children: ReactNode;
}

/**
 * DeployLockProvider component provides the deploy lock state to its children components.
 * It establishes a WebSocket connection to the server to listen for deploy lock status changes.
 *
 * @param {ReactNode} children - The children components that will receive the deploy lock state.
 * @returns {JSX.Element} - The DeployLockProvider component.
 */
export function DeployLockProvider({children}: DeployLockProviderProps): JSX.Element {
    const [isDeployLocked, setIsDeployLocked] = useState<boolean>(false);
    const [socket, setSocket] = useState<WebSocket | null>(null);
    /**
     * Establishes a WebSocket connection to the server.
     */
    useEffect(() => {
        const browserWindow = (globalThis as typeof globalThis & { window?: Window }).window;
        if (browserWindow === undefined) {
            return undefined;
        }
        if (!browserWindow.location) {
            return undefined;
        }
        const { protocol: pageProtocol, host, hostname, port } = browserWindow.location;
        const wsProtocol = pageProtocol === 'https:' ? 'wss:' : 'ws:';
        const isDevelopment = (hostname === '127.0.0.1' || hostname === 'localhost')
            && port === '3000'; // Checking if we are running in development mode
        const wsUrl = isDevelopment
            ? `${wsProtocol}//127.0.0.1:8080/ws`  // Development WebSocket URL
            : `${wsProtocol}//${host}/ws`;        // Production WebSocket URL
        const newSocket = new WebSocket(wsUrl);
        setSocket(newSocket);

        return () => {
            newSocket.close();
        };
    }, []);

    /**
     * Listens for deploy lock status changes from the server.
     */
    useEffect(() => {
        if (socket) {
            socket.onopen = async () => {
                const lock = await fetchDeployLock();
                setIsDeployLocked(lock);
            };
            socket.onmessage = (event) => {
                const message = event.data;
                if (message === 'locked') {
                    setIsDeployLocked(true);
                } else if (message === 'unlocked') {
                    setIsDeployLocked(false);
                }
            };
        }
    }, [socket]);

    return (
        <DeployLockContext.Provider value={isDeployLocked}>
            {children}
        </DeployLockContext.Provider>
    );
}

/**
 * Custom hook to retrieve the deploy lock status from the context.
 *
 * @returns The deploy lock status as a boolean value.
 */
export function useDeployLock(): boolean {
    const context = useContext(DeployLockContext);
    if (context === undefined) {
        throw new Error('useDeployLock must be used within a DeployLockProvider');
    }
    return context;
}
