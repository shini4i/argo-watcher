import { useCallback, useEffect, useMemo, useState } from 'react';
import { HttpError } from 'react-admin';
import { buildQueryString, httpClient } from '../../data/httpClient';
import type { AppSummariesResponse, AppSummary, OverviewWindow } from './types';
import { WINDOW_SECONDS } from './types';

interface AppSummariesState {
  readonly apps: AppSummary[];
  /** True only before the first answer: there is nothing to render yet. */
  readonly isPending: boolean;
  /** True while a fetch runs over rows already on screen. */
  readonly isRefreshing: boolean;
  readonly error: unknown;
  readonly refetch: () => void;
}

/**
 * @description Loads the per-application aggregate for one window. Counts come
 * from the backend, so nothing here is a sample of a page. Rows from the
 * previous window stay until the next answer lands, which is what keeps a
 * window switch from unmounting the page.
 * @param window the selected look-back range
 * @returns the summaries, both loading flags, any error, and a refetch callback
 */
export const useAppSummaries = (window: OverviewWindow): AppSummariesState => {
  const [apps, setApps] = useState<AppSummary[]>([]);
  const [isFetching, setIsFetching] = useState(true);
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
    setIsFetching(true);
    // Retry replaces the failure, so the error screen must go with the click.
    setError(null);

    const query = buildQueryString({ from_timestamp: fromTimestamp });
    httpClient<AppSummariesResponse>(`/api/v1/apps/summary${query}`)
      .then(({ data, status }) => {
        if (cancelled) {
          return;
        }
        // A soft error in the body means the aggregate failed; surfacing it as
        // an error keeps the page from rendering zeros as if they were counts.
        // Stale rows go with it: they belong to a window nobody asked for.
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
          setIsFetching(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [fromTimestamp, reloadToken]);

  // Both flags key on what is currently on screen rather than on how many
  // fetches have run: a retry after a failure has nothing to show and owes the
  // reader a skeleton, exactly as the first load does.
  return {
    apps,
    isPending: isFetching && apps.length === 0,
    isRefreshing: isFetching && apps.length > 0,
    error,
    refetch,
  };
};
