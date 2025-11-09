import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { DeployLockListener } from './deployLockService';
import { DeployLockService, __testing } from './deployLockService';
import * as sharedUtils from '../../shared/utils';

class MockWebSocket {
  public onmessage: ((event: { data: string }) => void) | null = null;
  public onclose: (() => void) | null = null;
  public onerror: ((error: unknown) => void) | null = null;

  constructor(public url: string) {
    MockWebSocket.instances.push(this);
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

  it('fetches initial status and notifies subscribers', async () => {
    mockFetch([{ body: false }]);
    const service = new DeployLockService();
    const listener = vi.fn();
    service.subscribe(listener);

    await vi.waitUntil(() => listener.mock.calls.length > 0);
    expect(listener).toHaveBeenCalledWith(false);
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
