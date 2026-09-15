import { useServerConfig } from '../../data/serverConfig';

/**
 * @returns whether OIDC is enabled, or `null` while the server configuration is
 * unknown — in flight or failed — so callers gate privileged actions conservatively
 * (treating "unknown" as "denied") instead of falling open.
 */
export const useOidcEnabled = (): boolean | null => {
  const { config } = useServerConfig();

  // Collapsing an unknown configuration to false would let ConfigDrawer treat OIDC as
  // disabled and allow unauthenticated toggling of the deploy lock.
  if (config === null) {
    return null;
  }

  return Boolean(config.oidc?.enabled);
};
