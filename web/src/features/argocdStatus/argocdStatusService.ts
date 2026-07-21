import { httpClient } from '../../data/httpClient';
import { resolveWebSocketUrl } from '../../data/webSocketUrl';
import { getBrowserWindow } from '../../shared/utils';

/**
 * Subscribed listener signature invoked whenever ArgoCD reachability changes.
 * `available` is true when argo-watcher can reach ArgoCD.
 */
export type ArgocdStatusListener = (available: boolean) => void;

const WS_RETRY_DELAY_MS = 5000;

/**
 * WebSocket messages the server pushes on reachability transitions. Kept in sync
 * with the backend (internal/server/env.go).
 */
const ARGOCD_DOWN_MESSAGE = 'argocd_down';
const ARGOCD_UP_MESSAGE = 'argocd_up';

/**
 * ArgocdStatusService mirrors the deploy-lock service for a different signal: it
 * bootstraps ArgoCD reachability over REST and then tracks live changes pushed
 * over the shared `/ws` WebSocket, so the frontend can surface an "ArgoCD
 * unreachable" banner (issue #498). It is read-only — there are no imperative
 * actions, unlike the deploy-lock service.
 */
export class ArgocdStatusService {
  private currentStatus: boolean | null = null;
  private readonly listeners = new Set<ArgocdStatusListener>();
  private socket: WebSocket | null = null;
  private reconnectHandle: number | null = null;
  // Ordering guards so an out-of-order async result can never revert the banner
  // to a stale value. Both matter because a (re)connect can have a bootstrap and
  // an onopen fetch in flight at once, alongside live WS pushes:
  //   fetchSeq     - only the most recently issued fetch may apply its result;
  //                  older concurrent fetches are dropped.
  //   wsGeneration - bumped on every WebSocket transition; a fetch is dropped if
  //                  one landed while it was in flight, so a slow REST response
  //                  cannot clobber a fresher live update.
  private fetchSeq = 0;
  private wsGeneration = 0;

  /**
   * Retrieves the latest reachability from the backend and notifies subscribers.
   * The result is applied only if it is still the newest fetch AND no WebSocket
   * transition landed while it was in flight; otherwise it is dropped so REST/WS
   * ordering races cannot revert the banner to a stale value.
   */
  public async fetchStatus(): Promise<boolean> {
    const seq = ++this.fetchSeq;
    const wsGen = this.wsGeneration;
    const response = await httpClient<boolean>('/api/v1/argocd-status');
    const available = Boolean(response.data);
    if (seq !== this.fetchSeq || wsGen !== this.wsGeneration) {
      return this.currentStatus ?? available;
    }
    this.setStatus(available);
    return available;
  }

  /**
   * Subscribes to reachability changes, establishing a WebSocket connection when
   * needed. Returns an unsubscribe function for convenient cleanup.
   */
  public subscribe(listener: ArgocdStatusListener): () => void {
    this.listeners.add(listener);

    if (this.currentStatus === null) {
      this.fetchStatus().catch(error => {
        console.error('[argocd-status] Failed to fetch initial status', error);
      });
    } else {
      listener(this.currentStatus);
    }

    this.ensureSocket();

    return () => {
      this.listeners.delete(listener);
      if (this.listeners.size > 0) {
        return;
      }
      this.teardownSocket();
    };
  }

  /** Broadcasts the new reachability to all subscribers. */
  private setStatus(available: boolean) {
    this.currentStatus = available;
    for (const listener of this.listeners) {
      listener(available);
    }
  }

  /** Ensures a websocket connection exists whenever there are active listeners. */
  private ensureSocket() {
    if (this.socket || this.listeners.size === 0) {
      return;
    }

    const url = resolveWebSocketUrl();
    this.socket = new WebSocket(url);

    // Re-bootstrap against the authoritative cached state on every (re)connect:
    // the server only pushes on transitions, so a transition during a socket
    // drop would otherwise leave the reconnected client with a stale banner —
    // and a false-negative here hides a real outage (issue #498).
    this.socket.onopen = () => {
      this.fetchStatus().catch(error => {
        console.error('[argocd-status] Failed to reconcile status on connect', error);
      });
    };

    this.socket.onmessage = event => {
      const payload = typeof event.data === 'string' ? event.data : '';
      if (payload === ARGOCD_DOWN_MESSAGE) {
        this.wsGeneration++;
        this.setStatus(false);
      } else if (payload === ARGOCD_UP_MESSAGE) {
        this.wsGeneration++;
        this.setStatus(true);
      }
    };

    this.socket.onclose = () => {
      this.socket = null;
      if (this.listeners.size > 0) {
        this.scheduleReconnect();
      }
    };

    this.socket.onerror = error => {
      console.error('[argocd-status] WebSocket error', error);
      this.socket?.close();
    };
  }

  /** Schedules a websocket reconnect attempt with basic backoff. */
  private scheduleReconnect() {
    if (this.reconnectHandle !== null) {
      return;
    }

    const browserWindow = getBrowserWindow();
    if (!browserWindow) {
      return;
    }

    this.reconnectHandle = browserWindow.setTimeout(() => {
      this.reconnectHandle = null;
      this.ensureSocket();
    }, WS_RETRY_DELAY_MS);
  }

  /** Closes any active websocket and cancels pending reconnect timers. */
  private teardownSocket() {
    if (this.reconnectHandle !== null) {
      const browserWindow = getBrowserWindow();
      browserWindow?.clearTimeout(this.reconnectHandle);
      this.reconnectHandle = null;
    }

    this.socket?.close();
    this.socket = null;
    // Forget the cached reachability so a later re-subscribe bootstraps a fresh
    // fetch instead of replaying a value that may have gone stale while nobody
    // was listening.
    this.currentStatus = null;
  }
}

/**
 * Shared singleton instance consumed by the React-admin UI.
 */
export const argocdStatusService = new ArgocdStatusService();
