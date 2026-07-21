import { getBrowserWindow } from '../shared/utils';

/**
 * Shared resolution of the argo-watcher `/ws` WebSocket endpoint. Both the
 * deploy-lock and ArgoCD-status features open a socket to the same endpoint, so
 * the (security-sensitive) host validation lives here once rather than being
 * copied per feature.
 */

const IS_DEV_BUILD = import.meta.env?.DEV ?? false;

/** Logs an invalid websocket configuration, hiding the raw value outside dev builds. */
const warnInvalidSocketConfig = (message: string, ...details: unknown[]) => {
  if (IS_DEV_BUILD) {
    console.warn(message, ...details);
  } else {
    console.warn('[websocket] Invalid websocket configuration; falling back to defaults.');
  }
};

/** Normalizes custom websocket base URLs ensuring they match the current origin and use safe protocols. */
const sanitizeCustomSocketBase = (rawBase: string, location?: Location | null): URL | null => {
  const trimmed = rawBase.trim();
  if (!trimmed) {
    return null;
  }

  try {
    let candidate: URL;
    if (trimmed.startsWith('http') || trimmed.startsWith('ws')) {
      candidate = new URL(trimmed);
    } else {
      const relativePath = trimmed.startsWith('/') ? trimmed : `/${trimmed}`;
      candidate = new URL(relativePath, location?.origin ?? 'http://localhost');
    }

    const isHttp = candidate.protocol === 'http:' || candidate.protocol === 'https:';
    const isWs = candidate.protocol === 'ws:' || candidate.protocol === 'wss:';

    if (!isHttp && !isWs) {
      warnInvalidSocketConfig('[websocket] Ignoring custom websocket URL with unsupported protocol:', candidate.protocol);
      return null;
    }

    if (location && candidate.host !== location.host) {
      warnInvalidSocketConfig(
        `[websocket] Ignoring custom websocket URL (${candidate.origin}) because it does not match the current host (${location.host}).`,
      );
      return null;
    }

    return candidate;
  } catch (error) {
    warnInvalidSocketConfig('[websocket] Invalid custom websocket URL, falling back to window.location.', error);
    return null;
  }
};

/** Ensures the socket URL ends with `/ws` and uses the websocket protocol scheme. */
const toSocketUrl = (url: URL): string => {
  const normalizedPath = url.pathname.endsWith('/ws')
    ? url.pathname
    : `${url.pathname.replace(/\/$/, '') || ''}/ws`;

  let protocol: 'ws:' | 'wss:';
  if (url.protocol === 'https:') {
    protocol = 'wss:';
  } else if (url.protocol === 'http:') {
    protocol = 'ws:';
  } else {
    protocol = url.protocol as 'ws:' | 'wss:';
  }
  return `${protocol}//${url.host}${normalizedPath}`;
};

/** Determines the websocket endpoint based on env overrides or current location, validating custom hosts. */
export const resolveWebSocketUrl = (): string => {
  const location = getBrowserWindow()?.location ?? null;
  const base = import.meta.env.VITE_WS_BASE_URL ?? '';
  if (base) {
    const sanitized = sanitizeCustomSocketBase(base, location);
    if (sanitized) {
      return toSocketUrl(sanitized);
    }
  }

  const protocol = location?.protocol === 'https:' ? 'wss:' : 'ws:';
  const host = location?.host ?? 'localhost';
  return `${protocol}//${host}/ws`;
};
