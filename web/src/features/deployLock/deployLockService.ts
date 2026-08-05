import { httpClient, type HttpResponse } from '../../data/httpClient';
import { resolveWebSocketUrl, webSocketProtocols } from '../../data/webSocketUrl';
import { getBrowserWindow } from '../../shared/utils';

/**
 * Subscribed listener signature invoked whenever the deploy-lock status changes.
 */
export type DeployLockListener = (locked: boolean) => void;

const WS_RETRY_DELAY_MS = 5000;

/**
 * DeployLockService coordinates REST and WebSocket interactions for the deploy-lock feature,
 * exposing subscription hooks and imperative helpers for lock operations.
 */
export class DeployLockService {
  private currentStatus: boolean | null = null;
  private readonly listeners = new Set<DeployLockListener>();
  private socket: WebSocket | null = null;
  private reconnectHandle: number | null = null;
  // Ordering guards so an out-of-order async result can never revert the banner
  // to a stale value. Both matter because a (re)connect can have a bootstrap and
  // an onopen fetch in flight at once, alongside live WS pushes and the operator's
  // own lock/release calls:
  //   fetchSeq        - only the most recently issued fetch may apply its result;
  //                     older concurrent fetches are dropped.
  //   stateGeneration - bumped on every authoritative update (WS push, lock,
  //                     release); a fetch is dropped if one landed while it was
  //                     in flight, so a slow REST response cannot clobber it.
  private fetchSeq = 0;
  private stateGeneration = 0;

  /**
   * Retrieves the latest lock state from the backend and notifies subscribers of
   * the result. The result is applied only if it is still the newest fetch AND no
   * authoritative update landed while it was in flight; otherwise it is dropped so
   * REST/WebSocket ordering races cannot revert the banner to a stale value.
   */
  public async fetchStatus(): Promise<boolean> {
    const seq = ++this.fetchSeq;
    const generation = this.stateGeneration;
    const response = await httpClient<boolean>('/api/v1/deploy-lock');
    const locked = Boolean(response.data);
    if (seq !== this.fetchSeq || generation !== this.stateGeneration) {
      return this.currentStatus ?? locked;
    }
    this.updateStatus(locked);
    return locked;
  }

  /**
   * Issues a POST request to enable the deploy lock and propagates the new state.
   */
  public async setLock(): Promise<HttpResponse<unknown>> {
    const response = await httpClient('/api/v1/deploy-lock', { method: 'POST' });
    this.applyAuthoritative(true);
    return response;
  }

  /**
   * Issues a DELETE request to release the deploy lock and propagates the new state.
   */
  public async releaseLock(): Promise<HttpResponse<unknown>> {
    const response = await httpClient('/api/v1/deploy-lock', { method: 'DELETE' });
    this.applyAuthoritative(false);
    return response;
  }

  /**
   * Subscribes to deploy-lock state changes, automatically establishing a WebSocket connection when needed.
   * Returns an unsubscribe function for convenient cleanup.
   */
  public subscribe(listener: DeployLockListener): () => void {
    this.listeners.add(listener);

    if (this.currentStatus === null) {
      this.fetchStatus().catch(error => {
        console.error('[deploy-lock] Failed to fetch initial status', error);
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

  /**
   * Applies a state we know first-hand — a live WebSocket push or the operator's
   * own lock/release — and invalidates any REST fetch still in flight, so a slower
   * response carrying the pre-change value cannot overwrite it.
   */
  private applyAuthoritative(locked: boolean) {
    this.stateGeneration++;
    this.updateStatus(locked);
  }

  /** Broadcasts the new lock status to all subscribers. */
  private updateStatus(locked: boolean) {
    this.currentStatus = locked;
    for (const listener of this.listeners) {
      listener(locked);
    }
  }

  /** Ensures a websocket connection exists whenever there are active listeners. */
  private ensureSocket() {
    if (this.socket || this.listeners.size === 0) {
      return;
    }

    const url = resolveWebSocketUrl();
    this.socket = new WebSocket(url, webSocketProtocols());

    // Re-bootstrap against the authoritative state on every (re)connect: the
    // server only pushes on transitions, and with the shared Postgres lock a
    // transition can come from another replica. A change that happened while this
    // socket was down — or before a failed bootstrap fetch — would otherwise leave
    // the client showing a stale state, and a false-negative here hides an active
    // deployment freeze.
    this.socket.onopen = () => {
      this.fetchStatus().catch(error => {
        console.error('[deploy-lock] Failed to reconcile status on connect', error);
      });
    };

    this.socket.onmessage = event => {
      const payload = typeof event.data === 'string' ? event.data : '';
      if (payload === 'locked') {
        this.applyAuthoritative(true);
      } else if (payload === 'unlocked') {
        this.applyAuthoritative(false);
      }
    };

    this.socket.onclose = () => {
      this.socket = null;
      if (this.listeners.size > 0) {
        this.scheduleReconnect();
      }
    };

    this.socket.onerror = error => {
      console.error('[deploy-lock] WebSocket error', error);
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
    // Forget the cached lock state so a later re-subscribe bootstraps a fresh
    // fetch instead of replaying a value that may have gone stale while nobody
    // was listening. Bumping fetchSeq invalidates any fetch issued before the
    // teardown, which would otherwise repopulate the cache as it resolves.
    this.fetchSeq++;
    this.currentStatus = null;
  }
}

/**
 * Shared singleton instance consumed by the React-admin UI.
 */
export const deployLockService = new DeployLockService();

/**
 * Internal testing hooks exposing non-exported helpers.
 */
export const __testing = {
  resolveWebSocketUrl,
};
