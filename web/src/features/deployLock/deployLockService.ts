import type { HttpResponse } from '../../data/httpClient';
import { httpClient } from '../../data/httpClient';
import { getBrowserWindow } from '../../shared/utils';

/**
 * Subscribed listener signature invoked whenever the deploy-lock status changes.
 */
export type DeployLockListener = (locked: boolean) => void;

const WS_RETRY_DELAY_MS = 5000;

/** Determines the websocket endpoint based on env overrides or current location. */
const resolveWebSocketUrl = () => {
  const base = import.meta.env.VITE_WS_BASE_URL ?? '';
  if (base) {
    return base.endsWith('/') ? `${base}ws` : `${base}/ws`;
  }

  const location = getBrowserWindow()?.location;
  const protocol = location?.protocol === 'https:' ? 'wss:' : 'ws:';
  const host = location?.host ?? 'localhost';
  return `${protocol}//${host}/ws`;
};

/**
 * DeployLockService coordinates REST and WebSocket interactions for the deploy-lock feature,
 * exposing subscription hooks and imperative helpers for lock operations.
 */
export class DeployLockService {
  private currentStatus: boolean | null = null;
  private readonly listeners = new Set<DeployLockListener>();
  private socket: WebSocket | null = null;
  private reconnectHandle: number | null = null;

  /**
   * Retrieves the latest lock state from the backend and notifies subscribers of the result.
   */
  public async fetchStatus(): Promise<boolean> {
    const response = await httpClient<boolean>('/api/v1/deploy-lock');
    const locked = Boolean(response.data);
    this.updateStatus(locked);
    return locked;
  }

  /**
   * Issues a POST request to enable the deploy lock and propagates the new state.
   */
  public async setLock(): Promise<HttpResponse<unknown>> {
    const response = await httpClient('/api/v1/deploy-lock', { method: 'POST' });
    this.updateStatus(true);
    return response;
  }

  /**
   * Issues a DELETE request to release the deploy lock and propagates the new state.
   */
  public async releaseLock(): Promise<HttpResponse<unknown>> {
    const response = await httpClient('/api/v1/deploy-lock', { method: 'DELETE' });
    this.updateStatus(false);
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
    this.socket = new WebSocket(url);

    this.socket.onmessage = event => {
      const payload = typeof event.data === 'string' ? event.data : '';
      if (payload === 'locked') {
        this.updateStatus(true);
      } else if (payload === 'unlocked') {
        this.updateStatus(false);
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
  }
}

/**
 * Shared singleton instance consumed by the React-admin UI.
 */
export const deployLockService = new DeployLockService();
