import React, { createContext, useContext, useEffect, useState, ReactNode } from 'react';

export async function fetchDeployLock(): Promise<boolean> {
  const response = await fetch('/api/v1/deploy-lock');
  return await response.json();
}

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
  children: ReactNode;
}

export function DeployLockProvider({ children }: DeployLockProviderProps): JSX.Element {
  const [deployLock, setDeployLockState] = useState<boolean>(false);
  const [socket, setSocket] = useState<WebSocket | null>(null);

  useEffect(() => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    const wsUrl = `${protocol}//${host}/ws`;
    const newSocket = new WebSocket(wsUrl);
    setSocket(newSocket);

    return () => {
      newSocket.close();
    };
  }, []);

  useEffect(() => {
    if (socket) {
      socket.onopen = async () => {
        const lock = await fetchDeployLock();
        setDeployLockState(lock);
      };
      socket.onmessage = (event) => {
        const message = event.data;
        if (message === 'locked') {
          setDeployLockState(true);
        } else if (message === 'unlocked') {
          setDeployLockState(false);
        }
      };
    }
  }, [socket]);

  return (
    <DeployLockContext.Provider value={deployLock}>
      {children}
    </DeployLockContext.Provider>
  );
}

export function useDeployLock(): boolean {
  const context = useContext(DeployLockContext);
  if (context === undefined) {
    throw new Error('useDeployLock must be used within a DeployLockProvider');
  }
  return context;
}
