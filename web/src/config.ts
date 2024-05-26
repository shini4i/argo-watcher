type KeycloakConfig = {
  token_validation_interval: number;
  enabled: boolean;
  url: string;
  realm: string;
  client_id: string;
  privileged_groups: string[];
};

type ConfigType = {
  keycloak: KeycloakConfig,
  [key: string]: any
};

let config: ConfigType | null = null;

export async function fetchConfig(): Promise<ConfigType> {
  if (!config) {
    const response = await fetch('/api/v1/config');
    const data = await response.json();
    config = data as ConfigType;
  }
  return config as ConfigType;
}
