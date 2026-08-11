import { resolveWebSocketUrl, webSocketProtocols } from './webSocketUrl';
import { getBrowserWindow } from '../shared/utils';

/** Subscribed listener signature invoked whenever the tracked state changes. */
export type WsStatusListener<T> = (state: T) => void;

const WS_RETRY_DELAY_MS = 5000;

/**
 * Base class for a server-owned signal that the UI mirrors: the state is
 * bootstrapped over REST, then kept current from frames pushed on the shared
 * `/ws` socket, and broadcast to subscribers. It owns the socket lifecycle
 * (single connection while listeners exist, reconnect with a fixed delay,
 * teardown when the last listener leaves) and the ordering guards below.
 *
 * Subclasses supply the transport specifics — {@link fetchState} for the REST
 * bootstrap and {@link parseMessage} for the frames they recognise — and may call
 * {@link applyAuthoritative} to publish a state this client set itself.
 *
 * `T` must be non-nullable: `null` is reserved for "nothing cached yet".
 */
export abstract class WsStatusService<T extends NonNullable<unknown>> {
  private currentStatus: T | null = null;
  private readonly listeners = new Set<WsStatusListener<T>>();
  private socket: WebSocket | null = null;
  private reconnectHandle: number | null = null;
  // Ordering guards so an out-of-order async result can never revert the state
  // to a stale value. Both matter because a (re)connect can have a bootstrap and
  // an onopen fetch in flight at once, alongside live WS pushes and any
  // imperative action this client takes:
  //   fetchSeq        - only the most recently issued fetch may apply its result;
  //                     older concurrent fetches are dropped.
  //   stateGeneration - bumped on every authoritative update (a recognised WS
  //                     frame, or an imperative action); a fetch is dropped if
  //                     one landed while it was in flight, so a slow REST
  //                     response cannot clobber it.
  private fetchSeq = 0;
  private stateGeneration = 0;

  /**
   * @param logPrefix Tag for this signal's console diagnostics, logged as `[<prefix>] …`.
   */
  protected constructor(private readonly logPrefix: string) {}

  /** Reads the authoritative state from the backend. */
  protected abstract fetchState(): Promise<T>;

  /**
   * Maps a text frame to a new state, or `undefined` when the frame belongs to
   * another signal sharing the socket. Returning `undefined` — rather than an
   * unchanged state — is what keeps unrelated traffic from invalidating a
   * reconcile that is still in flight.
   */
  protected abstract parseMessage(payload: string): T | undefined;

  /**
   * Retrieves the latest state from the backend and notifies subscribers of the
   * result. The result is applied only if it is still the newest fetch AND no
   * authoritative update landed while it was in flight; otherwise it is dropped
   * and the winning state is returned instead, so REST/WebSocket ordering races
   * cannot revert subscribers to a stale value.
   */
  public async fetchStatus(): Promise<T> {
    const seq = ++this.fetchSeq;
    const generation = this.stateGeneration;
    const state = await this.fetchState();
    if (seq !== this.fetchSeq || generation !== this.stateGeneration) {
      return this.currentStatus ?? state;
    }
    this.updateStatus(state);
    return state;
  }

  /**
   * Subscribes to state changes, establishing a WebSocket connection when needed.
   * Returns an unsubscribe function for convenient cleanup.
   */
  public subscribe(listener: WsStatusListener<T>): () => void {
    this.listeners.add(listener);

    if (this.currentStatus === null) {
      this.fetchStatus().catch(error => {
        console.error(`[${this.logPrefix}] Failed to fetch initial status`, error);
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
   * Applies a state we know first-hand — a recognised WebSocket frame or an
   * action this client performed — and invalidates any REST fetch still in
   * flight, so a slower response carrying the pre-change value cannot overwrite it.
   */
  protected applyAuthoritative(state: T) {
    this.stateGeneration++;
    this.updateStatus(state);
  }

  /** Broadcasts the new state to all subscribers. */
  private updateStatus(state: T) {
    this.currentStatus = state;
    for (const listener of this.listeners) {
      listener(state);
    }
  }

  /** Ensures a websocket connection exists whenever there are active listeners. */
  private ensureSocket() {
    if (this.socket || this.listeners.size === 0) {
      return;
    }

    const url = resolveWebSocketUrl();
    // Handlers close over this socket rather than reading `this.socket`, so a
    // retired connection can never act on behalf of its replacement.
    const socket = new WebSocket(url, webSocketProtocols());
    this.socket = socket;

    // Re-bootstrap against the authoritative state on every (re)connect: the
    // server only pushes on transitions, and with multiple replicas a transition
    // can come from another one. A change that happened while this socket was
    // down — or before a failed bootstrap fetch — would otherwise leave the
    // client showing a stale state.
    socket.onopen = () => {
      this.fetchStatus().catch(error => {
        console.error(`[${this.logPrefix}] Failed to reconcile status on connect`, error);
      });
    };

    socket.onmessage = event => {
      const payload = typeof event.data === 'string' ? event.data : '';
      const next = this.parseMessage(payload);
      if (next !== undefined) {
        this.applyAuthoritative(next);
      }
    };

    socket.onclose = () => {
      // A closing socket reports back asynchronously, so this can arrive after a
      // teardown already replaced it (React remounts a subscriber often enough
      // for that to be routine). Acting on it would abandon the live replacement
      // and open a third connection.
      if (this.socket !== socket) {
        return;
      }
      this.socket = null;
      if (this.listeners.size > 0) {
        this.scheduleReconnect();
      }
    };

    socket.onerror = error => {
      console.error(`[${this.logPrefix}] WebSocket error`, error);
      socket.close();
    };
  }

  /** Schedules a websocket reconnect attempt after a fixed delay. */
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
    // Forget the cached state so a later re-subscribe bootstraps a fresh fetch
    // instead of replaying a value that may have gone stale while nobody was
    // listening. Bumping fetchSeq invalidates any fetch issued before the
    // teardown, which would otherwise repopulate the cache as it resolves.
    this.fetchSeq++;
    this.currentStatus = null;
  }
}
