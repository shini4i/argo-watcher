import { useEffect } from 'react';

/**
 * Triggers the provided handler at the given interval (in seconds).
 * When the interval is falsy the handler is not scheduled.
 */
export const useAutoRefresh = (intervalSeconds: number, handler: () => void) => {
  useEffect(() => {
    if (!intervalSeconds) {
      return undefined;
    }

    const id = window.setInterval(handler, intervalSeconds * 1000);
    return () => window.clearInterval(id);
  }, [intervalSeconds, handler]);
};
