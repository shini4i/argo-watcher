import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { DeployLockListener } from './deployLockService';
import { DeployLockService, __testing } from './deployLockService';
import * as sharedUtils from '../../shared/utils';
import { clearAccessToken, setAccessToken } from '../../auth/tokenStore';

class MockWebSocket {
  public onopen: (() => void) | null = null;
  public onmessage: ((event: { data: string }) => void) | null = null;
  public onclose: (() => void) | null = null;
  public onerror: ((error: unknown) => void) | null = null;

  constructor(
    public url: string,
    public protocols?: string | string[],
  ) {
    MockWebSocket.instances.push(this);
  }

  public open() {
    this.onopen?.();
  }

  public close() {
    this.onclose?.();
  }

  public emit(message: string) {
    this.onmessage?.({ data: message });
  }

  static readonly instances: MockWebSocket[] = [];

  static reset() {
    MockWebSocket.instances.length = 0;
  }
}

describe('DeployLockService', () => {
  const originalEnv = { ...import.meta.env };

  beforeEach(() => {
    vi.restoreAllMocks();
    MockWebSocket.reset();
    vi.stubGlobal('WebSocket', MockWebSocket as unknown as typeof WebSocket);
    import.meta.env.VITE_WS_BASE_URL = originalEnv.VITE_WS_BASE_URL;
  });

  const mockFetch = (responses: Array<{ body: unknown; status?: number }>) => {
    const sequence = [...responses];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async () => {
      const next = sequence.shift() ?? { body: {}, status: 200 };
      return new Response(JSON.stringify(next.body), {
        status: next.status ?? 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
  };

  const jsonResponse = (body: unknown) => new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });

  // Installs a fetch mock whose responses stay pending until resolved by hand, so
  // a REST/WebSocket ordering race can be driven deterministically. Every call is
  // recorded in order, so a test can resolve two concurrent fetches out of order.
  const mockPendingFetch = () => {
    const pending: Array<(r: Response) => void> = [];
    let markStarted: () => void = () => {};
    const started = new Promise<void>(resolve => { markStarted = resolve; });
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      () => new Promise<Response>(resolve => {
        pending.push(resolve);
        markStarted();
      }),
    );
    return {
      started,
      count: () => pending.length,
      // Resolves the nth outstanding fetch (0 = the oldest).
      resolveNth: (index: number, body: unknown) => pending[index](jsonResponse(body)),
      resolveWith: (body: unknown) => pending[pending.length - 1](jsonResponse(body)),
    };
  };

  it('fetches initial status and notifies subscribers', async () => {
    mockFetch([{ body: false }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    service.subscribe(listener);

    await vi.waitUntil(() => listener.mock.calls.length > 0);
    expect(listener).toHaveBeenCalledWith(false);
  });

  it('offers the access token as a subprotocol on the handshake', async () => {
    // The browser cannot set a header here, so dropping this argument would make every
    // socket fail the handshake once OIDC is enabled.
    setAccessToken('abc.def.ghi');
    mockFetch([{ body: false }]);
    const service = new DeployLockService();
    const unsubscribe = service.subscribe(vi.fn());
    await Promise.resolve();

    expect(MockWebSocket.instances[0].protocols).toEqual([
      'argo-watcher.v1',
      'argo-watcher.token.abc.def.ghi',
    ]);

    unsubscribe();
    clearAccessToken();
  });

  it('updates status on WebSocket messages', async () => {
    mockFetch([{ body: false }]);
    const service = new DeployLockService();
    const listener: DeployLockListener = vi.fn();

    service.subscribe(listener);

    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    const socket = MockWebSocket.instances[0];

    socket.emit('locked');
    expect(listener).toHaveBeenLastCalledWith(true);

    socket.emit('unlocked');
    expect(listener).toHaveBeenLastCalledWith(false);
  });

  it('re-fetches the lock state whenever the socket (re)connects', async () => {
    // The server only pushes on transitions, and with the shared Postgres lock a
    // transition can be driven by another replica. A lock set while this client's
    // socket was down (or before its bootstrap fetch failed) would otherwise stay
    // invisible, hiding an active freeze.
    mockFetch([{ body: false }, { body: true }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    service.subscribe(listener);

    // Wait for the bootstrap fetch to land before clearing, so the assertion below
    // can only be satisfied by the onopen refetch.
    await vi.waitUntil(() => listener.mock.calls.length > 0);
    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    listener.mockClear();

    MockWebSocket.instances[0].open();

    await vi.waitUntil(() => listener.mock.calls.length > 0);
    expect(listener).toHaveBeenLastCalledWith(true);
  });

  it('does not let a slow REST fetch clobber a newer WebSocket transition', async () => {
    // A reconcile fetch is held pending while a WS transition lands, then resolves
    // with the now-stale value; the WS state must win.
    mockFetch([{ body: false }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    service.subscribe(listener);
    await vi.waitUntil(() => listener.mock.calls.length > 0);

    const { resolveWith, started } = mockPendingFetch();
    const pending = service.fetchStatus();
    await started;

    MockWebSocket.instances[0].emit('locked');
    expect(listener).toHaveBeenLastCalledWith(true);

    resolveWith(false);
    await pending;
    expect(listener).toHaveBeenLastCalledWith(true);
  });

  it('does not let a slow REST fetch clobber a just-issued lock operation', async () => {
    // A reconnect reconcile can be in flight when the operator hits lock; the
    // explicit operation is newer than the fetch and must not be reverted.
    mockFetch([{ body: false }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    service.subscribe(listener);
    await vi.waitUntil(() => listener.mock.calls.length > 0);

    const { resolveWith, started } = mockPendingFetch();
    const pending = service.fetchStatus();
    await started;

    mockFetch([{ body: 'ok' }]);
    await service.setLock();
    expect(listener).toHaveBeenLastCalledWith(true);

    resolveWith(false);
    await pending;
    expect(listener).toHaveBeenLastCalledWith(true);
  });

  it('re-bootstraps on a fresh subscribe after full teardown', async () => {
    mockFetch([{ body: false }, { body: true }]);
    const service = new DeployLockService();
    const unsubscribe = service.subscribe(vi.fn());

    await vi.waitUntil(() => (globalThis.fetch as unknown as vi.Mock).mock.calls.length === 1);
    unsubscribe(); // last subscriber leaves -> teardown clears cached state

    const listener = vi.fn();
    service.subscribe(listener);
    // currentStatus was reset, so a second bootstrap fetch must fire (not a replay
    // of a value that could have gone stale while nobody was listening).
    await vi.waitUntil(() => (globalThis.fetch as unknown as vi.Mock).mock.calls.length === 2);
    await vi.waitUntil(() => listener.mock.calls.some(call => call[0] === true));
    expect(listener).toHaveBeenLastCalledWith(true);
  });

  it('drops an older concurrent fetch that resolves after a newer one', async () => {
    // A (re)connect can have the bootstrap fetch and the onopen reconcile in flight
    // at once. If the older one resolves last it carries the pre-change value, and
    // applying it would revert the banner — hiding an active freeze.
    mockFetch([{ body: false }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    service.subscribe(listener);
    await vi.waitUntil(() => listener.mock.calls.length > 0);
    listener.mockClear();

    const { started, count, resolveNth } = mockPendingFetch();
    const older = service.fetchStatus();
    await started;
    const newer = service.fetchStatus();
    await vi.waitUntil(() => count() === 2);

    resolveNth(1, true);
    await expect(newer).resolves.toBe(true);
    expect(listener).toHaveBeenLastCalledWith(true);

    // The older fetch must be dropped, and report the state that won rather than
    // its own stale answer.
    resolveNth(0, false);
    await expect(older).resolves.toBe(true);
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it('does not let a slow REST fetch clobber a newer WebSocket release', async () => {
    // Mirror of the locking direction: a reconcile issued before the release
    // resolves with manual_lock still true and would falsely re-freeze the UI.
    mockFetch([{ body: true }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    service.subscribe(listener);
    await vi.waitUntil(() => listener.mock.calls.length > 0);

    const { resolveWith, started } = mockPendingFetch();
    const pending = service.fetchStatus();
    await started;

    MockWebSocket.instances[0].emit('unlocked');
    expect(listener).toHaveBeenLastCalledWith(false);

    resolveWith(true);
    await pending;
    expect(listener).toHaveBeenLastCalledWith(false);
  });

  it('does not let a slow REST fetch clobber a just-issued release operation', async () => {
    mockFetch([{ body: true }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    service.subscribe(listener);
    await vi.waitUntil(() => listener.mock.calls.length > 0);

    const { resolveWith, started } = mockPendingFetch();
    const pending = service.fetchStatus();
    await started;

    mockFetch([{ body: 'ok' }]);
    await service.releaseLock();
    expect(listener).toHaveBeenLastCalledWith(false);

    resolveWith(true);
    await pending;
    expect(listener).toHaveBeenLastCalledWith(false);
  });

  it('reconciles on a reconnect after the socket dropped', async () => {
    // The hole this closes: a lock set while the browser's socket was down. Only a
    // replacement socket's onopen can surface it, so exercise the real reconnect.
    mockFetch([{ body: false }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    service.subscribe(listener);
    await vi.waitUntil(() => listener.mock.calls.length > 0);
    await vi.waitUntil(() => MockWebSocket.instances.length === 1);

    const mockWindow = {
      setTimeout: vi.fn((cb: () => void) => {
        cb();
        return 1 as unknown as number;
      }),
      clearTimeout: vi.fn(),
    };
    const windowSpy = vi.spyOn(sharedUtils, 'getBrowserWindow')
      .mockReturnValue(mockWindow as unknown as Window);

    MockWebSocket.instances[0].onclose?.();
    await vi.waitUntil(() => MockWebSocket.instances.length === 2);

    // The lock was set while the socket was down.
    mockFetch([{ body: true }]);
    listener.mockClear();
    MockWebSocket.instances[1].open();

    await vi.waitUntil(() => listener.mock.calls.length > 0);
    expect(listener).toHaveBeenLastCalledWith(true);
    windowSpy.mockRestore();
  });

  it('ignores unrelated messages on the shared socket', async () => {
    // /ws also carries ArgoCD reachability frames, which are far more frequent than
    // lock frames. They must neither change the lock state nor invalidate an
    // in-flight reconcile — that would disable the reconcile during outages.
    mockFetch([{ body: false }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    service.subscribe(listener);
    await vi.waitUntil(() => listener.mock.calls.length > 0);
    listener.mockClear();

    const socket = MockWebSocket.instances[0];
    socket.emit('argocd_down:argocd');
    socket.emit('argocd_up');
    socket.emit('nonsense');
    expect(listener).not.toHaveBeenCalled();

    const { resolveWith, started } = mockPendingFetch();
    const pending = service.fetchStatus();
    await started;
    socket.emit('argocd_up');

    resolveWith(true);
    await pending;
    expect(listener).toHaveBeenLastCalledWith(true);
  });

  it('keeps the last known state when a reconnect reconcile fails', async () => {
    mockFetch([{ body: true }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    service.subscribe(listener);
    await vi.waitUntil(() => listener.mock.calls.length > 0);
    expect(listener).toHaveBeenLastCalledWith(true);
    listener.mockClear();

    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('boom'));
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    MockWebSocket.instances[0].open();

    await vi.waitUntil(() => errorSpy.mock.calls.length > 0);
    expect(errorSpy).toHaveBeenCalledWith(
      '[deploy-lock] Failed to reconcile status on connect', expect.any(Error),
    );
    // A failed reconcile must not clear the banner — that is the false negative the
    // reconcile exists to prevent.
    expect(listener).not.toHaveBeenCalled();
    errorSpy.mockRestore();
  });

  it('discards a fetch that resolves after teardown', async () => {
    // A fetch still in flight when the last subscriber leaves must not repopulate
    // the cache teardown just cleared, or the next subscribe replays that value
    // instead of bootstrapping a fresh one.
    mockFetch([{ body: false }]);
    const service = new DeployLockService();
    const unsubscribe = service.subscribe(vi.fn());
    await vi.waitUntil(() => (globalThis.fetch as unknown as vi.Mock).mock.calls.length === 1);

    const { resolveWith, started } = mockPendingFetch();
    const pending = service.fetchStatus();
    await started;

    unsubscribe();
    resolveWith(true);
    await pending; // the stale result has now been fully processed (or dropped)

    mockFetch([{ body: false }]);
    const listener = vi.fn();
    service.subscribe(listener);

    await vi.waitUntil(() => listener.mock.calls.length > 0);
    expect(listener).toHaveBeenLastCalledWith(false);
  });

  it('invokes REST helpers for set and release operations', async () => {
    mockFetch([{ body: false }, { body: 'ok' }, { body: 'ok' }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    service.subscribe(listener);

    await service.setLock();
    await service.releaseLock();

    const fetchCalls = (globalThis.fetch as unknown as vi.Mock).mock.calls;
    expect(fetchCalls[1][0]).toContain('/api/v1/deploy-lock');
    expect(fetchCalls[1][1]).toMatchObject({ method: 'POST' });
    expect(fetchCalls[2][1]).toMatchObject({ method: 'DELETE' });
  });

  it('tears down websocket when the last subscriber unsubscribes', async () => {
    mockFetch([{ body: false }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    const unsubscribe = service.subscribe(listener);

    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    const socketCloseSpy = vi.spyOn(MockWebSocket.instances[0], 'close');

    unsubscribe();
    await vi.waitUntil(() => socketCloseSpy.mock.calls.length > 0);
    expect(socketCloseSpy).toHaveBeenCalled();
  });

  it('schedules reconnects when the socket closes with active listeners', async () => {
    mockFetch([{ body: false }]);
    const service = new DeployLockService();
    const listenerA = vi.fn();
    const listenerB = vi.fn();

    service.subscribe(listenerA);
    service.subscribe(listenerB);

    await vi.waitUntil(() => MockWebSocket.instances.length === 1);

    const mockWindow = {
      setTimeout: vi.fn((cb: () => void) => {
        cb();
        return 1 as unknown as number;
      }),
      clearTimeout: vi.fn(),
    };
    const windowSpy = vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue(globalThis.window as Window);

    windowSpy.mockReturnValue(mockWindow as unknown as Window);
    MockWebSocket.instances[0].onclose?.();

    expect(mockWindow.setTimeout).toHaveBeenCalledWith(expect.any(Function), 5000);
    windowSpy.mockRestore();
  });

  it('logs errors when initial status fetch fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('boom'));
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const service = new DeployLockService();
    service.subscribe(() => {});

    await vi.waitUntil(() => errorSpy.mock.calls.length > 0);
    expect(errorSpy).toHaveBeenCalledWith('[deploy-lock] Failed to fetch initial status', expect.any(Error));
    errorSpy.mockRestore();
  });

  it('logs websocket errors and closes the socket', async () => {
    mockFetch([{ body: false }]);
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const service = new DeployLockService();
    service.subscribe(() => {});

    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    const socket = MockWebSocket.instances[0];
    const closeSpy = vi.spyOn(socket, 'close');

    const wsError = new Error('ws');
    socket.onerror?.(wsError);

    expect(errorSpy).toHaveBeenCalledWith('[deploy-lock] WebSocket error', wsError);
    expect(closeSpy).toHaveBeenCalled();
    errorSpy.mockRestore();
  });

  it('builds websocket URLs from env overrides and window location', () => {
    const customWindow = {
      location: {
        protocol: 'https:',
        host: 'custom.example',
      },
    } as unknown as Window;
    const windowSpy = vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue(customWindow);

    import.meta.env.VITE_WS_BASE_URL = 'wss://custom.example';
    expect(__testing.resolveWebSocketUrl()).toBe('wss://custom.example/ws');

    windowSpy.mockReturnValue({
      location: {
        protocol: 'https:',
        host: 'argo.example',
      },
    } as unknown as Window);
    import.meta.env.VITE_WS_BASE_URL = 'wss://malicious.example';
    expect(__testing.resolveWebSocketUrl()).toBe('wss://argo.example/ws');

    import.meta.env.VITE_WS_BASE_URL = '';
    expect(__testing.resolveWebSocketUrl()).toBe('wss://argo.example/ws');
    windowSpy.mockRestore();
  });
});
