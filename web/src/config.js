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
