let config = null;

export async function fetchConfig() {
    if (!config) {
        const response = await fetch('/api/v1/config');
        config = await response.json();
    }
    return config;
}

export async function fetchDeployLock() {
    const response = await fetch('/api/v1/deploy-lock');
    return await response.json();
}

export async function releaseDeployLock() {
    const response = await fetch('/api/v1/deploy-lock', {
        method: 'DELETE',
    });

    if (response.status !== 200) {
        throw new Error(`Error: ${response.status}`);
    }
}

export async function setDeployLock() {
    const response = await fetch('/api/v1/deploy-lock', {
        method: 'POST',
    });

    if (response.status !== 200) {
        throw new Error(`Error: ${response.status}`);
    }
}
