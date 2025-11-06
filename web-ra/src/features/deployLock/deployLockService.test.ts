import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { DeployLockListener } from './deployLockService';
import { DeployLockService } from './deployLockService';

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

  static instances: MockWebSocket[] = [];

  static reset() {
    MockWebSocket.instances = [];
  }
}

describe('DeployLockService', () => {
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
});
