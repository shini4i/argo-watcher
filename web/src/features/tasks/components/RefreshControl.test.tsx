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

  it('reseeds the countdown after a refetch instead of letting it run negative', () => {
    const onRefresh = vi.fn();
    renderWithProvider(<RefreshControl onRefresh={onRefresh} />, 2);

    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(onRefresh).toHaveBeenCalledTimes(1);
    expect(screen.getByText(/Live · 2s/)).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(onRefresh).toHaveBeenCalledTimes(2);
  });

  it('reseeds the countdown when the interval is changed', () => {
    const onRefresh = vi.fn();
    renderWithProvider(<RefreshControl onRefresh={onRefresh} />, 10);

    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(screen.getByText(/Live · 7s/)).toBeInTheDocument();

    // MUI's Select is a button + listbox, not a native <select>.
    fireEvent.mouseDown(screen.getByLabelText('Auto-refresh interval'));
    fireEvent.click(screen.getByRole('option', { name: '30s' }));
    expect(screen.getByText(/Live · 30s/)).toBeInTheDocument();
  });

  it('hydrates a stored interval that matches a presented option', () => {
    globalThis.localStorage.setItem('recentTasks.refreshInterval', '30');
    const onRefresh = vi.fn();
    renderWithProvider(<RefreshControl onRefresh={onRefresh} />, 10);

    // The label proves the countdown seed; the Select proves the hydration
    // effect reached the surrounding TaskListProvider.
    expect(screen.getByText(/Live · 30s/)).toBeInTheDocument();
    expect(screen.getByLabelText('Auto-refresh interval')).toHaveTextContent('30s');
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
