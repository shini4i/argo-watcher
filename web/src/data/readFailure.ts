import { normalizeError } from '../shared/utils';

/** A failed read, in the words the UI shows. */
export interface ReadFailure {
  /** One-line headline naming what failed. */
  readonly title: string;
  /** What the server (or the transport) reported. */
  readonly detail?: string;
  /** Argo Watcher's own suggested remedy — never server-supplied. */
  readonly hint?: string;
}

/**
 * The 503 `status` set by internal/server/handlers.go providerUnavailableMessage.
 * Keying on it, not the code alone, keeps the other 503 the API returns off this tab.
 */
const PROVIDER_UNAVAILABLE = 'authentication provider unavailable';

/** Server text lands in a fixed-width panel, so it is trimmed like authFailure's. */
const MAX_DETAIL_LENGTH = 300;

const clamp = (text: string): string =>
  text.length > MAX_DETAIL_LENGTH ? `${text.slice(0, MAX_DETAIL_LENGTH)}…` : text;

/** Reads the server's `status` field from an error body of unvalidated shape. */
const serverStatus = (body: unknown): string | undefined => {
  if (typeof body !== 'object' || body === null || !('status' in body)) {
    return undefined;
  }
  const { status } = body as { status: unknown };
  return typeof status === 'string' ? status : undefined;
};

/** @param error - anything thrown by the data provider. */
export const describeReadFailure = (error: unknown): ReadFailure => {
  const { message, status, details } = normalizeError(error);

  if (status === 503) {
    if (serverStatus(details) === PROVIDER_UNAVAILABLE) {
      return {
        title: 'Argo Watcher cannot verify your session',
        detail:
          'The server could not reach the identity provider to validate your token, so it is refusing every read.',
        hint: 'Your sign-in has not been rejected — this is a server-side outage. Check that the OIDC issuer is reachable from the Argo Watcher server (DNS and egress), then retry.',
      };
    }

    return {
      title: 'Argo Watcher is unavailable',
      detail: clamp(message),
      hint: 'The server reported itself as unavailable. Retry once it recovers.',
    };
  }

  if (status === 401 || status === 403) {
    return {
      title: 'This session is no longer accepted',
      detail: clamp(message),
      hint: 'Reload the page to sign in again.',
    };
  }

  // status 0 is what httpClient reports for a network drop and for its own
  // request-timeout abort; undefined means the throw carried no HTTP status.
  if (status === undefined || status === 0) {
    return {
      title: 'Could not reach the Argo Watcher server',
      detail: clamp(message),
      hint: 'Check that the server is reachable, then retry.',
    };
  }

  return {
    title: status >= 500 ? 'The Argo Watcher server returned an error' : 'The request was rejected',
    detail: `${clamp(message)} (HTTP ${status})`,
    hint: 'Retry, and check the Argo Watcher server logs if it keeps failing.',
  };
};
