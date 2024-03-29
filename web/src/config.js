let config = null;

export async function fetchConfig() {
  if (!config) {
    const response = await fetch('/api/v1/config');
    config = await response.json();
  }
  return config;
}
