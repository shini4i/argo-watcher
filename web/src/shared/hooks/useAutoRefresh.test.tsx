import { render } from '@testing-library/react';
import type { FC } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useAutoRefresh } from './useAutoRefresh';

const setIntervalMock = vi.fn();
const clearIntervalMock = vi.fn();
let browserWindow: { setInterval: typeof setIntervalMock; clearInterval: typeof clearIntervalMock } | undefined;

vi.mock('../utils', () => ({
  getBrowserWindow: () => browserWindow,
}));

const TestHarness: FC<{ interval: number; handler: () => void }> = ({ interval, handler }) => {
  useAutoRefresh(interval, handler);
  return null;
};

describe('useAutoRefresh', () => {
  beforeEach(() => {
    setIntervalMock.mockReset();
    clearIntervalMock.mockReset();
    browserWindow = {
      setInterval: setIntervalMock,
      clearInterval: clearIntervalMock,
    };
  });

  it('schedules the handler and clears it on unmount when enabled', () => {
    let intervalId = 0;
    let capturedHandler: (() => void) | undefined;
    setIntervalMock.mockImplementation((handler: () => void) => {
      capturedHandler = handler;
      intervalId += 1;
      return intervalId;
    });

    const handler = vi.fn();
    const { unmount } = render(<TestHarness interval={5} handler={handler} />);

    expect(setIntervalMock).toHaveBeenCalledWith(expect.any(Function), 5000);
    capturedHandler?.();
    expect(handler).toHaveBeenCalledTimes(1);

    unmount();
    expect(clearIntervalMock).toHaveBeenCalledWith(intervalId);
  });

  it('does not register intervals when disabled', () => {
    render(<TestHarness interval={0} handler={vi.fn()} />);
    expect(setIntervalMock).not.toHaveBeenCalled();
    expect(clearIntervalMock).not.toHaveBeenCalled();
  });

  it('bails out when the browser window is unavailable', () => {
    browserWindow = undefined;
    render(<TestHarness interval={10} handler={vi.fn()} />);
    expect(setIntervalMock).not.toHaveBeenCalled();
  });
});
