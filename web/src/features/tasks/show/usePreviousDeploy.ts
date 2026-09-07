import { useGetList } from 'react-admin';
import type { Task } from '../../../data/types';

// The window ends at this task, so the first rows back are already the older
// ones. Five is only headroom for a burst of same-second deploys sorting ahead
// of the predecessor — the list orders by `created` alone.
const LOOKBACK_ROWS = 5;

// GetTasks counts before it pages, and `tasks` carries no index on `app`, so an
// unbounded `from` makes every detail-page open scan the whole table. Bounding
// it lets the `created` index do the work; a previous deploy older than this is
// reported as none found.
const LOOKBACK_SECONDS = 90 * 24 * 60 * 60;

const QUERY_OPTS = { retry: false, refetchOnWindowFocus: false, staleTime: 60_000 } as const;

/**
 * @description Finds the most recent earlier task for the same app, so the
 * detail page can link to what was deployed before this one.
 * @param app the application name, or nothing to skip the query
 * @param taskId the task being viewed, excluded from the result
 * @param created its creation timestamp in seconds; only older tasks qualify
 * @returns the previous task, or null when there is none
 */
export const usePreviousDeploy = (
  app: string | null | undefined,
  taskId: string | undefined,
  created: number | null,
): Task | null => {
  const { data } = useGetList<Task>(
    'tasks',
    // Reaches past the provider's 24h default, which a previous deploy usually
    // predates, and stops at this task so newer deploys cannot fill the page.
    {
      pagination: { page: 1, perPage: LOOKBACK_ROWS },
      filter: { app, from: (created ?? 0) - LOOKBACK_SECONDS, to: created ?? 0 },
    },
    { ...QUERY_OPTS, enabled: Boolean(app) && created !== null },
  );

  if (!data || created === null) {
    return null;
  }

  return data.find(task => task.id !== taskId && task.created < created) ?? null;
};
