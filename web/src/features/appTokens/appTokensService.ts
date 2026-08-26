import { httpClient } from '../../data/httpClient';

const ENDPOINT = '/api/v1/app-tokens';

/**
 * An application deploy token as the server describes it. `secret` is present
 * only in the response that created the token — it is never stored, so this is
 * the one chance to show it.
 */
export interface AppToken {
  id: string;
  apps?: string[];
  all_apps: boolean;
  hint: string;
  description?: string;
  created_by: string;
  /** Unix milliseconds; 0 or absent where the server has no timestamp. */
  created_at: number;
  expires_at?: number;
  revoked_at?: number;
  last_used_at?: number;
  secret?: string;
}

/**
 * The scope a new token is asked for. `all_apps` and `apps` are mutually
 * exclusive; the server rejects a request setting both.
 */
export interface IssueAppTokenRequest {
  apps?: string[];
  all_apps?: boolean;
  description?: string;
  /** Omit or pass 0 for a token that never expires. */
  expires_in_days?: number;
}

/**
 * Guards against the Web UI's HTML catch-all being mistaken for the API.
 *
 * Where the token endpoints are not registered the request falls through to it,
 * which answers 200 with `text/html`; httpClient then yields `undefined` rather
 * than throwing. Treating that as success would report a revocation that never
 * happened, so an absent body is an error here, not an empty result.
 */
const assertAnswered = <T>(data: T | undefined, action: string): T => {
  if (data === undefined) {
    throw new Error(
      `The server did not answer the deploy token ${action}. Token management may not be available on this instance.`,
    );
  }

  return data;
};

/**
 * Lists every token, revoked ones included. Never carries a secret.
 *
 * An absent body means the SPA catch-all answered, not the API — see assertAnswered.
 */
export const listAppTokens = async (): Promise<AppToken[]> => {
  const response = await httpClient<AppToken[]>(ENDPOINT);
  return assertAnswered(response.data, 'list');
};

/** Mints a token. The returned `secret` is the only copy that will ever exist. */
export const issueAppToken = async (request: IssueAppTokenRequest): Promise<AppToken> => {
  const response = await httpClient<AppToken>(ENDPOINT, { method: 'POST', body: request });
  if (!response.data) {
    throw new Error('The server issued a token but returned no payload.');
  }
  return response.data;
};

/**
 * Withdraws a token, effective on its next use.
 *
 * Confirming the API answered matters most here: reporting a revocation that did not
 * happen would tell an operator a leaked credential is dead while it still deploys.
 */
export const revokeAppToken = async (id: string): Promise<void> => {
  const response = await httpClient(`${ENDPOINT}/${encodeURIComponent(id)}`, { method: 'DELETE' });
  assertAnswered(response.data, 'revoke');
};

/** True while the token still authorizes deployments. */
export const isTokenActive = (token: AppToken, now: number = Date.now()): boolean => {
  if (token.revoked_at) {
    return false;
  }
  return !token.expires_at || token.expires_at > now;
};

/** Human-readable scope: the wildcard, or the application list. */
export const describeScope = (token: AppToken): string => {
  if (token.all_apps) {
    return 'All applications';
  }
  return token.apps?.length ? token.apps.join(', ') : 'No applications';
};
