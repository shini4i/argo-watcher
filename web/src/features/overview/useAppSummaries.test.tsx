import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useAppSummaries } from './useAppSummaries';
import { WINDOW_SECONDS } from './types';

const httpClient = vi.fn();
vi.mock('../../data/httpClient', async importOriginal => ({
  ...(await importOriginal<typeof import('../../data/httpClient')>()),
  httpClient: (...args: unknown[]) => httpClient(...args),
}));

const ok = (body: unknown) => Promise.resolve({ data: body, status: 200, headers: {} as Headers });

const lastUrl = () => httpClient.mock.calls.at(-1)![0] as string;

const NOW_MS = Date.parse('2026-09-07T12:00:00Z');

describe('useAppSummaries', () => {
  beforeEach(() => {
    httpClient.mockReset();
    httpClient.mockImplementation(() => ok({ apps: [], total_apps: 0 }));
    // Date.now is pinned rather than the whole timer set: waitFor polls on real
    // timers, and faking them starves it.
    vi.spyOn(Date, 'now').mockReturnValue(NOW_MS);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('asks the backend for the selected window', async () => {
    renderHook(() => useAppSummaries('7d'));

    await waitFor(() => expect(httpClient).toHaveBeenCalled());
    const params = new URL(lastUrl(), 'https://example.test').searchParams;
    const expected = Math.floor(NOW_MS / 1000) - WINDOW_SECONDS['7d'];
    expect(params.get('from_timestamp')).toBe(String(expected));
  });

  it('returns the apps the backend reports', async () => {
    httpClient.mockImplementation(() =>
      ok({ apps: [{ app: 'checkout', total: 3 }], total_apps: 1 }),
    );

    const { result } = renderHook(() => useAppSummaries('24h'));

    await waitFor(() => expect(result.current.isPending).toBe(false));
    expect(result.current.apps).toHaveLength(1);
    expect(result.current.error).toBeNull();
  });

  // Rendering zeros after a failed aggregate would read as "all clear".
  it('treats a soft error in the body as a failure and keeps the list empty', async () => {
    httpClient.mockImplementation(() =>
      ok({ apps: [], total_apps: 0, error: 'failed to aggregate app summaries' }),
    );

    const { result } = renderHook(() => useAppSummaries('24h'));

    await waitFor(() => expect(result.current.isPending).toBe(false));
    expect(result.current.error).toBeTruthy();
    expect(result.current.apps).toEqual([]);
  });

  it('reports a transport failure and clears stale rows', async () => {
    httpClient.mockImplementation(() => Promise.reject(new Error('network down')));

    const { result } = renderHook(() => useAppSummaries('24h'));

    await waitFor(() => expect(result.current.isPending).toBe(false));
    expect(result.current.error).toBeInstanceOf(Error);
    expect(result.current.apps).toEqual([]);
  });

  it('refetches on demand', async () => {
    const { result } = renderHook(() => useAppSummaries('24h'));
    await waitFor(() => expect(httpClient).toHaveBeenCalledTimes(1));

    await act(async () => {
      result.current.refetch();
    });

    await waitFor(() => expect(httpClient).toHaveBeenCalledTimes(2));
  });

  it('refetches when the window changes', async () => {
    const { rerender } = renderHook(({ w }: { w: '24h' | '30d' }) => useAppSummaries(w), {
      initialProps: { w: '24h' as const },
    });
    await waitFor(() => expect(httpClient).toHaveBeenCalledTimes(1));

    rerender({ w: '30d' });

    await waitFor(() => expect(httpClient).toHaveBeenCalledTimes(2));
    const params = new URL(lastUrl(), 'https://example.test').searchParams;
    const expected = Math.floor(NOW_MS / 1000) - WINDOW_SECONDS['30d'];
    expect(params.get('from_timestamp')).toBe(String(expected));
  });

  // A late response from an abandoned window must not overwrite the current one.
  it('ignores an in-flight response once the window has moved on', async () => {
    const resolvers: Array<(value: unknown) => void> = [];
    httpClient.mockImplementation(() => new Promise(resolve => { resolvers.push(resolve); }));

    const { result, rerender } = renderHook(
      ({ w }: { w: '24h' | '30d' }) => useAppSummaries(w),
      { initialProps: { w: '24h' as const } },
    );
    await waitFor(() => expect(resolvers).toHaveLength(1));

    rerender({ w: '30d' });
    await waitFor(() => expect(resolvers).toHaveLength(2));

    // The 30d answer lands first, then the abandoned 24h one.
    await act(async () => {
      resolvers[1]({ data: { apps: [{ app: 'current' }], total_apps: 1 }, status: 200, headers: {} });
      resolvers[0]({ data: { apps: [{ app: 'stale' }], total_apps: 1 }, status: 200, headers: {} });
    });

    expect(result.current.apps.map(app => app.app)).toEqual(['current']);
  });
});
