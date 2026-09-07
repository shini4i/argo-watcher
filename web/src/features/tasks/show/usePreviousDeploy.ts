import { useGetList } from 'react-admin';
import type { Task } from '../../../data/types';

/** Enough rows to look past this task itself without paging. */
const LOOKBACK_ROWS = 5;

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
    // from: 0 overrides the provider's 24h default — a previous deploy is often older.
    { pagination: { page: 1, perPage: LOOKBACK_ROWS }, filter: { app, from: 0 } },
    { ...QUERY_OPTS, enabled: Boolean(app) && created !== null },
  );

  if (!data || created === null) {
    return null;
  }

  return data.find(task => task.id !== taskId && task.created < created) ?? null;
};
