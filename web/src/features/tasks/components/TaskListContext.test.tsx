import { act, render, renderHook, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { TaskListProvider, useTaskListContext, usePauseRefresh } from './TaskListContext';

const wrapper = ({ children }: { children: React.ReactNode }) => (
  <TaskListProvider>{children}</TaskListProvider>
);

describe('TaskListContext', () => {
  it('tracks pause reasons as a set', () => {
    const { result } = renderHook(() => useTaskListContext(), { wrapper });
    expect(result.current.state.pausedReasons.size).toBe(0);

    act(() => result.current.pause('hover'));
    expect(result.current.state.pausedReasons.has('hover')).toBe(true);

    act(() => result.current.pause('hover'));
    expect(result.current.state.pausedReasons.size).toBe(1);

    act(() => result.current.pause('expand'));
    expect(result.current.state.pausedReasons.size).toBe(2);

    act(() => result.current.resume('hover'));
    expect(result.current.state.pausedReasons.has('hover')).toBe(false);
    expect(result.current.state.pausedReasons.has('expand')).toBe(true);
  });

  it('updates the interval and bumps lastRefetchedAt', () => {
    const { result } = renderHook(() => useTaskListContext(), { wrapper });
    const before = result.current.state.lastRefetchedAt;
    act(() => result.current.setInterval(10));
    expect(result.current.state.intervalSec).toBe(10);
    expect(result.current.state.lastRefetchedAt).toBeGreaterThanOrEqual(before);
  });

  it('falls back to a no-op controller without a provider', () => {
    const { result } = renderHook(() => useTaskListContext());
    expect(() => result.current.pause('x')).not.toThrow();
    expect(result.current.state.pausedReasons.size).toBe(0);
  });

  it('usePauseRefresh registers a pause for the component lifetime', () => {
    const Pauser = ({ active }: { active: boolean }) => {
      usePauseRefresh('hover', active);
      return null;
    };
    const Probe = () => {
      const ctx = useTaskListContext();
      return <span data-testid="reasons">{Array.from(ctx.state.pausedReasons).join(',')}</span>;
    };

    const { rerender, unmount } = render(
      <TaskListProvider>
        <Pauser active />
        <Probe />
      </TaskListProvider>,
    );
    expect(screen.getByTestId('reasons').textContent).toBe('hover');

    rerender(
      <TaskListProvider>
        <Pauser active={false} />
        <Probe />
      </TaskListProvider>,
    );
    expect(screen.getByTestId('reasons').textContent).toBe('');
    unmount();
  });
});
