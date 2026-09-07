import { renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Task } from '../../../data/types';
import { usePreviousDeploy } from './usePreviousDeploy';

const useGetList = vi.fn();
vi.mock('react-admin', () => ({ useGetList: (...args: unknown[]) => useGetList(...args) }));

const task = (id: string, created: number): Task => ({
  id,
  app: 'demo',
  author: 'alice',
  project: 'demo',
  created,
  updated: created + 60,
  images: [],
});

describe('usePreviousDeploy', () => {
  beforeEach(() => {
    useGetList.mockReset();
    useGetList.mockReturnValue({ data: undefined });
  });

  it('reaches past the provider default 24h window', () => {
    renderHook(() => usePreviousDeploy('demo', 'current', 1000));

    const [, params] = useGetList.mock.calls[0] as [string, { filter: Record<string, number | string> }];
    expect(params.filter.app).toBe('demo');
    // Bounded, not unbounded: GetTasks counts before it pages and `tasks` has
    // no index on `app`, so an open-ended `from` scans the whole table.
    expect(params.filter.from).toBe(1000 - 90 * 24 * 60 * 60);
  });

  it('asks only for tasks up to this one, so newer deploys cannot fill the page', () => {
    renderHook(() => usePreviousDeploy('demo', 'current', 1000));

    const [, params] = useGetList.mock.calls[0] as [string, { filter: Record<string, number> }];
    expect(params.filter.to).toBe(1000);
  });

  it('skips the query when there is no app to scope it to', () => {
    renderHook(() => usePreviousDeploy(undefined, 'current', 1000));

    const [, , options] = useGetList.mock.calls[0] as [string, unknown, { enabled: boolean }];
    expect(options.enabled).toBe(false);
  });

  it('returns the newest task older than the one being viewed', () => {
    useGetList.mockReturnValue({
      data: [task('newer', 3000), task('current', 2000), task('older', 1000), task('oldest', 500)],
    });

    const { result } = renderHook(() => usePreviousDeploy('demo', 'current', 2000));
    expect(result.current?.id).toBe('older');
  });

  it('never returns the task being viewed', () => {
    useGetList.mockReturnValue({ data: [task('current', 2000)] });

    const { result } = renderHook(() => usePreviousDeploy('demo', 'current', 2000));
    expect(result.current).toBeNull();
  });

  it('returns null when every candidate is newer', () => {
    useGetList.mockReturnValue({ data: [task('newer', 5000), task('newest', 6000)] });

    const { result } = renderHook(() => usePreviousDeploy('demo', 'current', 2000));
    expect(result.current).toBeNull();
  });

  // The upper bound is inclusive, so a burst of same-second deploys is the only
  // thing that can crowd the page; the predecessor still has to be found past it.
  it('looks past same-second siblings of the task being viewed', () => {
    useGetList.mockReturnValue({
      data: [task('sibling-a', 2000), task('current', 2000), task('sibling-b', 2000), task('older', 1500)],
    });

    const { result } = renderHook(() => usePreviousDeploy('demo', 'current', 2000));
    expect(result.current?.id).toBe('older');
  });

  it('returns null before the current task has a timestamp', () => {
    useGetList.mockReturnValue({ data: [task('older', 1000)] });

    const { result } = renderHook(() => usePreviousDeploy('demo', 'current', null));
    expect(result.current).toBeNull();
  });
});
