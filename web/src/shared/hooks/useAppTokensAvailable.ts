import { useServerConfig } from '../../data/serverConfig';

/**
 * @returns whether the server exposes the application deploy token endpoints, which need
 * both OIDC and the Postgres state backend, or `null` while the configuration is unknown.
 *
 * Without this the endpoints are not routes at all, and a request for them falls through
 * to the Web UI's HTML catch-all — which answers 200, so the token list would render empty
 * rather than failing, claiming no tokens exist on a server that cannot hold any.
 */
export const useAppTokensAvailable = (): boolean | null => {
  const { config } = useServerConfig();

  // Collapsing an unknown configuration to false would hide the feature on a transient
  // error, and to true would offer a broken page.
  if (config === null) {
    return null;
  }

  return Boolean(config.oidc?.enabled) && config.state_type === 'postgres';
};
