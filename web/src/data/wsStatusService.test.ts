import { beforeEach, describe, expect, it, vi } from 'vitest';
import { WsStatusService } from './wsStatusService';
import { resolveWebSocketUrl, webSocketProtocols } from './webSocketUrl';
import { MockWebSocket } from '../test/mockWebSocket';
import * as sharedUtils from '../shared/utils';

/** Promise whose settlement a test drives by hand, to hold a fetch in flight. */
const deferred = <T>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(res => {
    resolve = res;
  });
  return { promise, resolve };
};

/**
 * Concrete subclass exercising the shared protocol. `fetchState` is a spy so a
 * test can count bootstraps and hold one in flight; `parseMessage` recognises
 * only `state:<value>` frames, standing in for a real service ignoring the other
 * signals that share the `/ws` socket.
 */
class TestService extends WsStatusService<string> {
  public readonly fetchState = vi.fn<() => Promise<string>>(() => Promise.resolve('initial'));

  constructor(logPrefix = 'test-signal') {
    super(logPrefix);
  }

  protected parseMessage(payload: string): string | undefined {
    return payload.startsWith('state:') ? payload.slice('state:'.length) : undefined;
  }

  /** Stands in for a feature service's imperative action (e.g. deploy-lock's setLock). */
  public push(state: string) {
    this.applyAuthoritative(state);
  }
}

/** Subclass over a falsy-capable state, guarding the `false` vs `undefined` distinction. */
class BooleanService extends WsStatusService<boolean> {
  public readonly fetchState = vi.fn<() => Promise<boolean>>(() => Promise.resolve(false));

  constructor() {
    super('bool-signal');
  }

  protected parseMessage(payload: string): boolean | undefined {
    if (payload === 'on') {
      return true;
    }
    if (payload === 'off') {
      return false;
    }
    return undefined;
  }
}

/**
 * A browser never runs the callback before `setTimeout` returns, so firing it
 * synchronously would leave the service's timer handle assigned after the timer
 * had already run — hiding whether a second reconnect can be armed.
 */
const recordingWindow = () => {
  const pendingTimers: Array<() => void> = [];
  let nextHandle = 1;
  return {
    setTimeout: vi.fn((cb: () => void) => {
      pendingTimers.push(cb);
      return nextHandle++ as unknown as number;
    }),
    clearTimeout: vi.fn(),
    flush: () => {
      pendingTimers.splice(0, pendingTimers.length).forEach(cb => cb());
    },
  };
};

describe('WsStatusService', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    MockWebSocket.reset();
    vi.stubGlobal('WebSocket', MockWebSocket as unknown as typeof WebSocket);
  });

  const subscribedService = async () => {
    const service = new TestService();
    const listener = vi.fn();
    service.subscribe(listener);
    await vi.waitUntil(() => listener.mock.calls.length > 0);
    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    return { service, listener, socket: MockWebSocket.instances[0] };
  };

  it('bootstraps over REST on the first subscribe and notifies the listener', async () => {
    const { service, listener } = await subscribedService();

    expect(listener).toHaveBeenCalledWith('initial');
    expect(service.fetchState).toHaveBeenCalledTimes(1);
  });

  it('replays the cached state to a later subscriber without refetching', async () => {
    const { service } = await subscribedService();

    const second = vi.fn();
    service.subscribe(second);

    expect(second).toHaveBeenCalledWith('initial');
    expect(service.fetchState).toHaveBeenCalledTimes(1);
  });

  it('opens a single socket however many listeners subscribe', async () => {
    const { service } = await subscribedService();

    service.subscribe(vi.fn());
    service.subscribe(vi.fn());

    expect(MockWebSocket.instances).toHaveLength(1);
  });

  it('applies parsed WebSocket payloads and ignores unrecognised ones', async () => {
    const { listener, socket } = await subscribedService();
    listener.mockClear();

    socket.emit('state:changed');
    expect(listener).toHaveBeenLastCalledWith('changed');

    // Frames belonging to another signal on the shared socket.
    socket.emit('locked');
    socket.emit('argocd_down:argocd');
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it('ignores a frame whose payload is not text', async () => {
    // A binary frame must be treated as unrelated traffic: no state change, and
    // no invalidation of a reconcile in flight.
    const { service, listener, socket } = await subscribedService();
    listener.mockClear();

    const pending = deferred<string>();
    service.fetchState.mockReturnValueOnce(pending.promise);
    const inFlight = service.fetchStatus();

    socket.emitRaw(new ArrayBuffer(8));

    pending.resolve('reconciled');
    await inFlight;
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener).toHaveBeenLastCalledWith('reconciled');
  });

  it('applies a parsed falsy state instead of mistaking it for an unrelated frame', async () => {
    const service = new BooleanService();
    const listener = vi.fn();
    service.subscribe(listener);
    await vi.waitUntil(() => listener.mock.calls.length > 0);
    await vi.waitUntil(() => MockWebSocket.instances.length === 1);

    MockWebSocket.instances[0].emit('on');
    expect(listener).toHaveBeenLastCalledWith(true);

    MockWebSocket.instances[0].emit('off');
    expect(listener).toHaveBeenLastCalledWith(false);
  });

  it('replays a cached falsy state without refetching', async () => {
    const service = new BooleanService();
    const first = vi.fn();
    service.subscribe(first);
    await vi.waitUntil(() => first.mock.calls.length > 0);
    expect(first).toHaveBeenCalledWith(false);

    const second = vi.fn();
    service.subscribe(second);

    expect(second).toHaveBeenCalledWith(false);
    expect(service.fetchState).toHaveBeenCalledTimes(1);
  });

  it('does not let a slow fetch clobber a newer WebSocket transition', async () => {
    const { service, listener, socket } = await subscribedService();

    const pending = deferred<string>();
    service.fetchState.mockReturnValueOnce(pending.promise);
    const inFlight = service.fetchStatus();

    socket.emit('state:live');
    expect(listener).toHaveBeenLastCalledWith('live');

    pending.resolve('stale');
    await inFlight;
    expect(listener).toHaveBeenLastCalledWith('live');
  });

  it('does not let a slow fetch clobber a newer imperative update', async () => {
    const { service, listener } = await subscribedService();

    const pending = deferred<string>();
    service.fetchState.mockReturnValueOnce(pending.promise);
    const inFlight = service.fetchStatus();

    service.push('operator');
    expect(listener).toHaveBeenLastCalledWith('operator');

    pending.resolve('stale');
    await inFlight;
    expect(listener).toHaveBeenLastCalledWith('operator');
  });

  it('lets an unrecognised frame pass without invalidating an in-flight fetch', async () => {
    // Frames for the other signals on /ws are far more frequent than this
    // service's own. Treating them as transitions would cancel every reconcile.
    const { service, listener, socket } = await subscribedService();
    listener.mockClear();

    const pending = deferred<string>();
    service.fetchState.mockReturnValueOnce(pending.promise);
    const inFlight = service.fetchStatus();

    socket.emit('argocd_up');

    pending.resolve('reconciled');
    await inFlight;
    expect(listener).toHaveBeenLastCalledWith('reconciled');
  });

  it('drops an older concurrent fetch that resolves last and reports the winning state', async () => {
    const { service, listener } = await subscribedService();
    listener.mockClear();

    const older = deferred<string>();
    const newer = deferred<string>();
    service.fetchState.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise);
    const olderCall = service.fetchStatus();
    const newerCall = service.fetchStatus();

    newer.resolve('winner');
    await expect(newerCall).resolves.toBe('winner');

    older.resolve('loser');
    await expect(olderCall).resolves.toBe('winner');
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener).toHaveBeenLastCalledWith('winner');
  });

  it('reconciles over REST whenever the socket connects', async () => {
    // The server only pushes on transitions, so a change made while the socket
    // was down is visible only through a reconnect refetch.
    const { service, listener, socket } = await subscribedService();
    listener.mockClear();
    service.fetchState.mockResolvedValueOnce('reconnected');

    socket.open();

    await vi.waitUntil(() => listener.mock.calls.length > 0);
    expect(listener).toHaveBeenLastCalledWith('reconnected');
  });

  it('schedules a reconnect when the socket closes with listeners still attached', async () => {
    const { socket } = await subscribedService();
    const browserWindow = recordingWindow();
    vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue(browserWindow as unknown as Window);

    socket.close();

    expect(browserWindow.setTimeout).toHaveBeenCalledWith(expect.any(Function), 5000);
    expect(MockWebSocket.instances).toHaveLength(1);

    browserWindow.flush();
    expect(MockWebSocket.instances).toHaveLength(2);
  });

  it('arms only one reconnect timer while one is already pending', async () => {
    const { socket } = await subscribedService();
    const browserWindow = recordingWindow();
    vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue(browserWindow as unknown as Window);

    socket.close();
    socket.close();

    expect(browserWindow.setTimeout).toHaveBeenCalledTimes(1);
  });

  it('arms a fresh reconnect after an earlier drop already reconnected', async () => {
    const { socket } = await subscribedService();
    const browserWindow = recordingWindow();
    vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue(browserWindow as unknown as Window);

    socket.close();
    browserWindow.flush();
    await vi.waitUntil(() => MockWebSocket.instances.length === 2);

    // The replacement socket drops too: the timer handle must have been released
    // when the first one fired, or this reconnect is never armed.
    MockWebSocket.instances[1].close();
    expect(browserWindow.setTimeout).toHaveBeenCalledTimes(2);

    browserWindow.flush();
    expect(MockWebSocket.instances).toHaveLength(3);
  });

  it('arms a reconnect again after a teardown cancelled a pending one', async () => {
    // Teardown must release the handle it cancels; leaving it set would make
    // scheduleReconnect early-return forever and freeze subscribers on stale state.
    const service = new TestService();
    const unsubscribe = service.subscribe(vi.fn());
    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    const browserWindow = recordingWindow();
    vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue(browserWindow as unknown as Window);

    MockWebSocket.instances[0].close();
    expect(browserWindow.setTimeout).toHaveBeenCalledTimes(1);
    unsubscribe();

    service.subscribe(vi.fn());
    await vi.waitUntil(() => MockWebSocket.instances.length === 2);
    MockWebSocket.instances[1].close();

    expect(browserWindow.setTimeout).toHaveBeenCalledTimes(2);
  });

  it('does not schedule a reconnect outside a browser', async () => {
    // getBrowserWindow returning undefined must not throw inside the close
    // callback, where nothing would handle it.
    const { socket } = await subscribedService();
    vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue(undefined);

    expect(() => socket.close()).not.toThrow();
    expect(MockWebSocket.instances).toHaveLength(1);
  });

  it('reconciles through a real reconnect after the socket dropped', async () => {
    const { service, listener, socket } = await subscribedService();
    const browserWindow = recordingWindow();
    vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue(browserWindow as unknown as Window);

    socket.close();
    browserWindow.flush();
    await vi.waitUntil(() => MockWebSocket.instances.length === 2);

    listener.mockClear();
    service.fetchState.mockResolvedValueOnce('changed while down');
    MockWebSocket.instances[1].open();

    await vi.waitUntil(() => listener.mock.calls.length > 0);
    expect(listener).toHaveBeenLastCalledWith('changed while down');
  });

  it('ignores a delayed close from a socket a re-subscribe already replaced', async () => {
    // A real socket reports closed asynchronously, so a connection retired by
    // teardown can deliver its close after a re-subscribe opened the replacement.
    // React remounting a subscriber makes that sequence routine.
    const service = new TestService();
    const unsubscribe = service.subscribe(vi.fn());
    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    const retired = MockWebSocket.instances[0];
    retired.deferClose = true;
    const browserWindow = recordingWindow();
    vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue(browserWindow as unknown as Window);

    unsubscribe();
    const unsubscribeReplacement = service.subscribe(vi.fn());
    await vi.waitUntil(() => MockWebSocket.instances.length === 2);
    const replacement = MockWebSocket.instances[1];

    retired.fireClose();

    expect(MockWebSocket.instances).toHaveLength(2);
    expect(browserWindow.setTimeout).not.toHaveBeenCalled();

    // The replacement is still the tracked socket, so teardown can still close
    // it; had the stale close won, it would stay open with nobody holding it.
    const closeSpy = vi.spyOn(replacement, 'close');
    unsubscribeReplacement();
    expect(closeSpy).toHaveBeenCalled();
  });

  it('does not reconnect when the socket closes after the last listener left', async () => {
    const service = new TestService();
    const unsubscribe = service.subscribe(vi.fn());
    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    const browserWindow = recordingWindow();
    vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue(browserWindow as unknown as Window);

    unsubscribe();

    expect(MockWebSocket.instances).toHaveLength(1);
    expect(browserWindow.setTimeout).not.toHaveBeenCalled();
  });

  it('closes the socket and clears the cache when the last listener leaves', async () => {
    const service = new TestService();
    const unsubscribe = service.subscribe(vi.fn());
    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    const closeSpy = vi.spyOn(MockWebSocket.instances[0], 'close');

    unsubscribe();
    expect(closeSpy).toHaveBeenCalled();

    // The cache is dropped, so the next subscribe must bootstrap rather than
    // replay a value that may have gone stale while nobody was listening.
    service.fetchState.mockResolvedValueOnce('fresh');
    const listener = vi.fn();
    service.subscribe(listener);

    await vi.waitUntil(() => listener.mock.calls.length > 0);
    expect(service.fetchState).toHaveBeenCalledTimes(2);
    expect(listener).toHaveBeenLastCalledWith('fresh');
  });

  it('keeps the socket while other listeners remain', async () => {
    const { service, socket } = await subscribedService();
    const closeSpy = vi.spyOn(socket, 'close');
    const unsubscribeSecond = service.subscribe(vi.fn());

    unsubscribeSecond();

    expect(closeSpy).not.toHaveBeenCalled();
  });

  it('cancels a pending reconnect timer on teardown', async () => {
    const service = new TestService();
    const unsubscribe = service.subscribe(vi.fn());
    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    const browserWindow = recordingWindow();
    vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue(browserWindow as unknown as Window);

    // Left unflushed, so the handle is still outstanding when teardown runs.
    MockWebSocket.instances[0].close();
    const handle = browserWindow.setTimeout.mock.results[0].value;

    unsubscribe();

    expect(browserWindow.clearTimeout).toHaveBeenCalledWith(handle);
  });

  it('discards a fetch that resolves after teardown', async () => {
    const service = new TestService();
    const unsubscribe = service.subscribe(vi.fn());
    await vi.waitUntil(() => service.fetchState.mock.calls.length === 1);

    const pending = deferred<string>();
    service.fetchState.mockReturnValueOnce(pending.promise);
    const inFlight = service.fetchStatus();

    unsubscribe();
    pending.resolve('stale');
    await inFlight;

    // Had the stale result repopulated the cache, this subscribe would replay it
    // instead of bootstrapping.
    service.fetchState.mockResolvedValueOnce('fresh');
    const listener = vi.fn();
    service.subscribe(listener);

    await vi.waitUntil(() => listener.mock.calls.length > 0);
    expect(listener).toHaveBeenLastCalledWith('fresh');
  });

  it('keeps the last known state when a reconnect reconcile fails', async () => {
    const { service, listener, socket } = await subscribedService();
    listener.mockClear();
    service.fetchState.mockRejectedValueOnce(new Error('boom'));
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    socket.open();

    await vi.waitUntil(() => errorSpy.mock.calls.length > 0);
    expect(errorSpy).toHaveBeenCalledWith(
      '[test-signal] Failed to reconcile status on connect',
      expect.any(Error),
    );
    expect(listener).not.toHaveBeenCalled();
  });

  it('logs a failed bootstrap under the configured prefix', async () => {
    const service = new TestService('custom-prefix');
    service.fetchState.mockRejectedValueOnce(new Error('boom'));
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    service.subscribe(vi.fn());

    await vi.waitUntil(() => errorSpy.mock.calls.length > 0);
    expect(errorSpy).toHaveBeenCalledWith(
      '[custom-prefix] Failed to fetch initial status',
      expect.any(Error),
    );
  });

  it('logs socket errors under the configured prefix and closes the socket', async () => {
    const { socket } = await subscribedService();
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const closeSpy = vi.spyOn(socket, 'close');
    const wsError = new Error('ws');

    socket.onerror?.(wsError);

    expect(errorSpy).toHaveBeenCalledWith('[test-signal] WebSocket error', wsError);
    expect(closeSpy).toHaveBeenCalled();
  });

  it('offers the shared handshake protocols on the initial socket and on reconnects', async () => {
    // Dropping the protocols on a reconnect would fail the handshake once OIDC is
    // enabled, since a browser cannot set a header here.
    const { socket } = await subscribedService();
    expect(socket.url).toBe(resolveWebSocketUrl());
    expect(socket.protocols).toEqual(webSocketProtocols());

    const browserWindow = recordingWindow();
    vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue(browserWindow as unknown as Window);
    socket.close();
    browserWindow.flush();

    await vi.waitUntil(() => MockWebSocket.instances.length === 2);
    expect(MockWebSocket.instances[1].protocols).toEqual(webSocketProtocols());
  });
});
