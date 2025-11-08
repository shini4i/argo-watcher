import { useEffect } from 'react';
import { getBrowserWindow } from '../utils';

/**
 * Triggers the provided handler at the given interval (in seconds).
 * When the interval is falsy the handler is not scheduled.
 */
export const useAutoRefresh = (intervalSeconds: number, handler: () => void) => {
  useEffect(() => {
    if (!intervalSeconds) {
      return undefined;
    }

    const browserWindow = getBrowserWindow();
    if (!browserWindow) {
      return undefined;
    }

    const id = browserWindow.setInterval(handler, intervalSeconds * 1000);
    return () => browserWindow.clearInterval(id);
  }, [intervalSeconds, handler]);
};
