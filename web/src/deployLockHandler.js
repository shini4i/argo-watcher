import React, { createContext, useContext, useEffect, useState } from 'react';

export async function fetchDeployLock() {
  const response = await fetch('/api/v1/deploy-lock');
  return await response.json();
}

export async function releaseDeployLock(keycloakToken) {
  let headers = {
    'Content-Type': 'application/json',
  };

  if (keycloakToken !== null) {
    headers['Authorization'] = keycloakToken;
  }

  const response = await fetch('/api/v1/deploy-lock', {
    method: 'DELETE',
    headers: headers,
  });

  if (response.status !== 200) {
    throw new Error(`Error: ${response.status}`);
  }
}

export async function setDeployLock(keycloakToken = null) {
  let headers = {
    'Content-Type': 'application/json',
  };

  if (keycloakToken !== null) {
    headers['Authorization'] = keycloakToken;
  }

  const response = await fetch('/api/v1/deploy-lock', {
    method: 'POST',
    headers: headers,
  });

  if (response.status !== 200) {
    throw new Error(`Error: ${response.status}`);
  }
}

export const DeployLockContext = createContext(false);

export function DeployLockProvider({ children }) {
  const [deployLock, setDeployLock] = useState(false);
  const [socket, setSocket] = useState(null);

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
        setDeployLock(lock);
      };
      socket.onmessage = (event) => {
        const message = event.data;
        if (message === 'locked') {
          setDeployLock(true);
        } else if (message === 'unlocked') {
          setDeployLock(false);
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

export function useDeployLock() {
  return useContext(DeployLockContext);
}
