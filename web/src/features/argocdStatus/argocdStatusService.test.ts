import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ArgocdStatusListener } from './argocdStatusService';
import { ArgocdStatusService } from './argocdStatusService';
import * as sharedUtils from '../../shared/utils';

class MockWebSocket {
  public onopen: (() => void) | null = null;
  public onmessage: ((event: { data: string }) => void) | null = null;
  public onclose: (() => void) | null = null;
  public onerror: ((error: unknown) => void) | null = null;

  constructor(public url: string) {
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

describe('ArgocdStatusService', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    MockWebSocket.reset();
    vi.stubGlobal('WebSocket', MockWebSocket as unknown as typeof WebSocket);
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

  it('fetches initial reachability and notifies subscribers', async () => {
    mockFetch([{ body: true }]);
    const service = new ArgocdStatusService();
    const listener = vi.fn();
    service.subscribe(listener);

    await vi.waitUntil(() => listener.mock.calls.length > 0);
    expect(listener).toHaveBeenCalledWith(true);
  });

  it('fetches the argocd-status endpoint', async () => {
    mockFetch([{ body: true }]);
    const service = new ArgocdStatusService();
    service.subscribe(vi.fn());

    await vi.waitUntil(() => (globalThis.fetch as unknown as vi.Mock).mock.calls.length > 0);
    const fetchCalls = (globalThis.fetch as unknown as vi.Mock).mock.calls;
    expect(fetchCalls[0][0]).toContain('/api/v1/argocd-status');
  });

  it('updates reachability on WebSocket messages', async () => {
    mockFetch([{ body: true }]);
    const service = new ArgocdStatusService();
    const listener: ArgocdStatusListener = vi.fn();

    service.subscribe(listener);

    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    const socket = MockWebSocket.instances[0];

    socket.emit('argocd_down');
    expect(listener).toHaveBeenLastCalledWith(false);

    socket.emit('argocd_up');
    expect(listener).toHaveBeenLastCalledWith(true);
  });

  it('ignores unrelated WebSocket messages', async () => {
    mockFetch([{ body: true }]);
    const service = new ArgocdStatusService();
    const listener = vi.fn();

    service.subscribe(listener);
    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    listener.mockClear();

    // deploy-lock messages share the socket but must not move the argocd state.
    MockWebSocket.instances[0].emit('locked');
    MockWebSocket.instances[0].emit('unlocked');

    expect(listener).not.toHaveBeenCalled();
  });

  it('re-fetches reachability whenever the socket (re)connects', async () => {
    // A transition missed during a socket drop must be reconciled on reconnect,
    // otherwise the banner stays stale and hides a real outage.
    mockFetch([{ body: true }, { body: false }]);
    const service = new ArgocdStatusService();
    const listener = vi.fn();
    service.subscribe(listener);

    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    listener.mockClear();

    // Server now reports unreachable; the reconnect handshake must pick it up.
    MockWebSocket.instances[0].open();

    await vi.waitUntil(() => listener.mock.calls.length > 0);
    expect(listener).toHaveBeenLastCalledWith(false);
  });

  it('does not let a slow REST bootstrap clobber a newer WebSocket transition', async () => {
    // Bootstrap fetch is held pending while a WS transition lands, then resolves
    // with the now-stale value; the WS state must win.
    let resolveFetch: (r: Response) => void = () => {};
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      () => new Promise<Response>(resolve => { resolveFetch = resolve; }),
    );
    const service = new ArgocdStatusService();
    const listener = vi.fn();
    service.subscribe(listener); // fetchStatus is now in flight

    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    MockWebSocket.instances[0].emit('argocd_down');
    expect(listener).toHaveBeenLastCalledWith(false);

    // The stale bootstrap now resolves "reachable" — it must be discarded.
    resolveFetch(new Response(JSON.stringify(true), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }));
    await Promise.resolve();
    await Promise.resolve();
    expect(listener).toHaveBeenLastCalledWith(false);
  });

  it('re-bootstraps on a fresh subscribe after full teardown', async () => {
    mockFetch([{ body: true }, { body: false }]);
    const service = new ArgocdStatusService();
    const unsubscribe = service.subscribe(vi.fn());

    await vi.waitUntil(() => (globalThis.fetch as unknown as vi.Mock).mock.calls.length === 1);
    unsubscribe(); // last subscriber leaves -> teardown clears cached state

    const listener = vi.fn();
    service.subscribe(listener);
    // currentStatus was reset, so a second bootstrap fetch must fire (not a replay).
    await vi.waitUntil(() => (globalThis.fetch as unknown as vi.Mock).mock.calls.length === 2);
    await vi.waitUntil(() => listener.mock.calls.some(c => c[0] === false));
    expect(listener).toHaveBeenLastCalledWith(false);
  });

  it('tears down websocket when the last subscriber unsubscribes', async () => {
    mockFetch([{ body: true }]);
    const service = new ArgocdStatusService();
    const listener = vi.fn();
    const unsubscribe = service.subscribe(listener);

    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    const socketCloseSpy = vi.spyOn(MockWebSocket.instances[0], 'close');

    unsubscribe();
    await vi.waitUntil(() => socketCloseSpy.mock.calls.length > 0);
    expect(socketCloseSpy).toHaveBeenCalled();
  });

  it('schedules reconnects when the socket closes with active listeners', async () => {
    mockFetch([{ body: true }]);
    const service = new ArgocdStatusService();
    service.subscribe(vi.fn());
    service.subscribe(vi.fn());

    await vi.waitUntil(() => MockWebSocket.instances.length === 1);

    const mockWindow = {
      setTimeout: vi.fn((cb: () => void) => {
        cb();
        return 1 as unknown as number;
      }),
      clearTimeout: vi.fn(),
    };
    const windowSpy = vi
      .spyOn(sharedUtils, 'getBrowserWindow')
      .mockReturnValue(mockWindow as unknown as Window);

    MockWebSocket.instances[0].onclose?.();

    expect(mockWindow.setTimeout).toHaveBeenCalledWith(expect.any(Function), 5000);
    windowSpy.mockRestore();
  });

  it('logs errors when initial status fetch fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('boom'));
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const service = new ArgocdStatusService();
    service.subscribe(() => {});

    await vi.waitUntil(() => errorSpy.mock.calls.length > 0);
    expect(errorSpy).toHaveBeenCalledWith('[argocd-status] Failed to fetch initial status', expect.any(Error));
    errorSpy.mockRestore();
  });

  it('logs websocket errors and closes the socket', async () => {
    mockFetch([{ body: true }]);
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const service = new ArgocdStatusService();
    service.subscribe(() => {});

    await vi.waitUntil(() => MockWebSocket.instances.length === 1);
    const socket = MockWebSocket.instances[0];
    const closeSpy = vi.spyOn(socket, 'close');

    const wsError = new Error('ws');
    socket.onerror?.(wsError);

    expect(errorSpy).toHaveBeenCalledWith('[argocd-status] WebSocket error', wsError);
    expect(closeSpy).toHaveBeenCalled();
    errorSpy.mockRestore();
  });
});
