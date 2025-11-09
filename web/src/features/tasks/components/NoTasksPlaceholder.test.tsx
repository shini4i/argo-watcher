import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { NoTasksPlaceholder } from './NoTasksPlaceholder';

const useListContextMock = vi.fn();

vi.mock('react-admin', () => ({
  useListContext: () => useListContextMock(),
}));

type IntervalHandler = Parameters<typeof setInterval>[0];

const stubWindowTimers = (
  setIntervalImpl: (handler: IntervalHandler, ms?: number) => number,
  clearIntervalImpl: (id: number) => void,
) => {
  const originalWindow = globalThis.window as Window | undefined;
  if (!originalWindow) {
    throw new Error('window is not defined in this test environment');
  }
  const previousSetInterval = originalWindow.setInterval;
  const previousClearInterval = originalWindow.clearInterval;
  originalWindow.setInterval = setIntervalImpl as typeof originalWindow.setInterval;
  originalWindow.clearInterval = clearIntervalImpl as typeof originalWindow.clearInterval;
  return () => {
    originalWindow.setInterval = previousSetInterval;
    originalWindow.clearInterval = previousClearInterval;
  };
};

describe('NoTasksPlaceholder', () => {
  beforeEach(() => {
    useListContextMock.mockReset();
  });

  afterEach(() => {
    useListContextMock.mockReset();
  });

  it('schedules refetch calls and clears the interval on unmount', async () => {
    const handlers: IntervalHandler[] = [];
    const setIntervalMock = vi.fn((handler: IntervalHandler) => {
      handlers.push(handler);
      return handlers.length;
    });
    const clearIntervalMock = vi.fn();
    const restoreWindow = stubWindowTimers(setIntervalMock, clearIntervalMock);
    const refetch = vi.fn().mockResolvedValue(undefined);
    useListContextMock.mockReturnValue({ refetch });

    const { unmount } = render(
      <NoTasksPlaceholder title="Waiting" description="Hang tight" reloadIntervalMs={1000} />,
    );

    expect(screen.getByText(/Auto-refreshing every 1 seconds/)).toBeInTheDocument();
    expect(setIntervalMock).toHaveBeenCalledWith(expect.any(Function), 1000);

    await handlers[0]?.();
    await waitFor(() => expect(refetch).toHaveBeenCalledTimes(1));

    unmount();
    expect(clearIntervalMock).toHaveBeenCalledWith(1);
    restoreWindow();
  });

  it('avoids scheduling when refetch is unavailable', () => {
    const setIntervalMock = vi.fn();
    const clearIntervalMock = vi.fn();
    const restoreWindow = stubWindowTimers(setIntervalMock, clearIntervalMock);
    useListContextMock.mockReturnValue({});

    render(<NoTasksPlaceholder title="Title" description="Body" />);

    expect(setIntervalMock).not.toHaveBeenCalled();
    expect(clearIntervalMock).not.toHaveBeenCalled();
    restoreWindow();
  });
});
