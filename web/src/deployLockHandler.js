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
