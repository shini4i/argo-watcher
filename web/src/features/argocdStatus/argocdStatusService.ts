import { httpClient } from '../../data/httpClient';
import { WsStatusService, type WsStatusListener } from '../../data/wsStatusService';

/**
 * Which subsystem argo-watcher cannot reach, when unavailable. Mirrors the
 * backend argocd.Reason* constants (internal/argocd/argo.go); `null` means the
 * cause is unknown (e.g. a legacy down message without a suffix).
 */
export type ArgocdUnavailableReason = 'argocd' | 'database' | 'both' | null;

/**
 * Reachability snapshot broadcast to subscribers. `available` is true when
 * argo-watcher can reach both ArgoCD and its state backend; otherwise `reason`
 * names the unreachable subsystem so the banner can be specific.
 */
export interface ArgocdStatus {
  available: boolean;
  reason: ArgocdUnavailableReason;
}

/** Subscribed listener signature invoked whenever reachability changes. */
export type ArgocdStatusListener = WsStatusListener<ArgocdStatus>;

/** Shape of the /api/v1/reachability response body (reason omitted when up). */
interface ArgocdStatusResponse {
  available?: boolean;
  reason?: string;
}

/**
 * WebSocket messages the server pushes on reachability transitions. A down
 * message may carry the cause as a suffix ("argocd_down:<reason>"). Kept in sync
 * with the backend (internal/server/env.go).
 */
const ARGOCD_DOWN_MESSAGE = 'argocd_down';
const ARGOCD_UP_MESSAGE = 'argocd_up';
const ARGOCD_DOWN_PREFIX = `${ARGOCD_DOWN_MESSAGE}:`;

/** Canonical "everything reachable" snapshot, reused to avoid re-allocation. */
const AVAILABLE_STATUS: ArgocdStatus = { available: true, reason: null };

/** Narrows an arbitrary reason string to a known ArgocdUnavailableReason. */
const parseReason = (raw: string | undefined | null): ArgocdUnavailableReason => {
  switch (raw) {
    case 'argocd':
    case 'database':
    case 'both':
      return raw;
    default:
      return null;
  }
};

/** Builds a status snapshot from the reachability endpoint response body. */
const toStatus = (data: ArgocdStatusResponse | null | undefined): ArgocdStatus =>
  data?.available
    ? AVAILABLE_STATUS
    : { available: false, reason: parseReason(data?.reason) };

/**
 * ArgocdStatusService tracks whether argo-watcher can reach ArgoCD and its state
 * backend, so the frontend can surface an "ArgoCD unreachable" banner (issue
 * #498). It is read-only — there are no imperative actions, unlike the
 * deploy-lock service.
 */
export class ArgocdStatusService extends WsStatusService<ArgocdStatus> {
  constructor() {
    super('argocd-status');
  }

  /** Reads current reachability from the backend. */
  protected async fetchState(): Promise<ArgocdStatus> {
    const response = await httpClient<ArgocdStatusResponse>('/api/v1/reachability');
    return toStatus(response.data);
  }

  /**
   * Recognises the reachability frames; the other signals on `/ws` are ignored.
   * A down frame without a suffix, or with a cause this build does not know
   * (forward-compat), still reports an outage with the reason narrowed to null.
   */
  protected parseMessage(payload: string): ArgocdStatus | undefined {
    if (payload === ARGOCD_UP_MESSAGE) {
      return AVAILABLE_STATUS;
    }
    if (payload === ARGOCD_DOWN_MESSAGE) {
      return { available: false, reason: null };
    }
    if (payload.startsWith(ARGOCD_DOWN_PREFIX)) {
      return { available: false, reason: parseReason(payload.slice(ARGOCD_DOWN_PREFIX.length)) };
    }
    return undefined;
  }
}

/**
 * Shared singleton instance consumed by the React-admin UI.
 */
export const argocdStatusService = new ArgocdStatusService();
