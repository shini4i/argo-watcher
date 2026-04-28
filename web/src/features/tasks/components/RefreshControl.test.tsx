import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RefreshControl } from './RefreshControl';
import { TaskListProvider, usePauseRefresh } from './TaskListContext';

const renderWithProvider = (ui: React.ReactNode, intervalSec = 10) =>
  render(<TaskListProvider initialIntervalSec={intervalSec}>{ui}</TaskListProvider>);

describe('RefreshControl', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    globalThis.localStorage.clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders a live indicator with the current countdown', () => {
    const onRefresh = vi.fn();
    renderWithProvider(<RefreshControl onRefresh={onRefresh} />, 10);
    expect(screen.getByText(/Live · 10s/)).toBeInTheDocument();
  });

  it('fires onRefresh exactly once when the countdown reaches zero', () => {
    const onRefresh = vi.fn();
    renderWithProvider(<RefreshControl onRefresh={onRefresh} />, 2);
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it('shows "Paused" label when interval is Off', () => {
    const onRefresh = vi.fn();
    renderWithProvider(<RefreshControl onRefresh={onRefresh} />, 0);
    expect(screen.getByText('Paused')).toBeInTheDocument();
  });

  it('freezes the countdown when a pause reason is registered', () => {
    const onRefresh = vi.fn();
    const Pauser = () => {
      usePauseRefresh('hover');
      return null;
    };
    renderWithProvider(
      <>
        <Pauser />
        <RefreshControl onRefresh={onRefresh} />
      </>,
      5,
    );
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(onRefresh).not.toHaveBeenCalled();
    expect(screen.getByText(/paused/i)).toBeInTheDocument();
  });

  it('ignores an unsupported persisted interval and keeps the provided default', () => {
    const onRefresh = vi.fn();
    globalThis.localStorage.setItem('recentTasks.refreshInterval', '7777');
    renderWithProvider(<RefreshControl onRefresh={onRefresh} />, 10);
    // The hydration guard rejects the stale value, so the live countdown
    // stays seeded from the provider's initialIntervalSec rather than
    // falling into an empty-Select / NaN-countdown state.
    expect(screen.getByText(/Live · 10s/)).toBeInTheDocument();
  });

  it('manual refresh resets the countdown', () => {
    const onRefresh = vi.fn();
    renderWithProvider(<RefreshControl onRefresh={onRefresh} />, 5);
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    fireEvent.click(screen.getByLabelText('Refresh now'));
    expect(onRefresh).toHaveBeenCalledTimes(1);
    expect(screen.getByText(/Live · 5s/)).toBeInTheDocument();
  });
});
