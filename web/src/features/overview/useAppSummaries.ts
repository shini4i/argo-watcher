import { useCallback, useEffect, useMemo, useState } from 'react';
import { HttpError } from 'react-admin';
import { buildQueryString, httpClient } from '../../data/httpClient';
import type { AppSummariesResponse, AppSummary, OverviewWindow } from './types';
import { WINDOW_SECONDS } from './types';

interface AppSummariesState {
  readonly apps: AppSummary[];
  readonly isPending: boolean;
  readonly error: unknown;
  readonly refetch: () => void;
}

/**
 * @description Loads the per-application aggregate for one window. Counts come
 * from the backend, so nothing here is a sample of a page.
 * @param window the selected look-back range
 * @returns the summaries, the pending flag, any error, and a refetch callback
 */
export const useAppSummaries = (window: OverviewWindow): AppSummariesState => {
  const [apps, setApps] = useState<AppSummary[]>([]);
  const [isPending, setIsPending] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [reloadToken, setReloadToken] = useState(0);

  const refetch = useCallback(() => setReloadToken(token => token + 1), []);

  // The window start is pinned per fetch rather than per render, so a re-render
  // cannot slide the range and make two numbers on screen disagree.
  const fromTimestamp = useMemo(
    () => Math.floor(Date.now() / 1000) - WINDOW_SECONDS[window],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [window, reloadToken],
  );

  // reloadToken is a dependency in its own right: two refetches inside the same
  // second compute the same fromTimestamp, and keying only on that would drop
  // the second one.
  useEffect(() => {
    let cancelled = false;
    setIsPending(true);

    const query = buildQueryString({ from_timestamp: fromTimestamp });
    httpClient<AppSummariesResponse>(`/api/v1/apps/summary${query}`)
      .then(({ data, status }) => {
        if (cancelled) {
          return;
        }
        // A soft error in the body means the aggregate failed; surfacing it as
        // an error keeps the page from rendering zeros as if they were counts.
        if (data?.error) {
          setError(new HttpError(data.error, status, data));
          setApps([]);
          return;
        }
        setError(null);
        setApps(data?.apps ?? []);
      })
      .catch(cause => {
        if (!cancelled) {
          setError(cause);
          setApps([]);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setIsPending(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [fromTimestamp, reloadToken]);

  return { apps, isPending, error, refetch };
};
