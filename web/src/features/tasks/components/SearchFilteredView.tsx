import { useMemo, type ReactNode } from 'react';
import { ListContextProvider, useListContext } from 'react-admin';
import type { Task } from '../../../data/types';
import { useTaskListContext } from './TaskListContext';

const matchesQuery = (task: Task, query: string): boolean => {
  const haystack = [
    task.app ?? '',
    task.author ?? '',
    ...(task.images?.map(img => `${img.image}:${img.tag}`) ?? []),
  ]
    .join(' ')
    .toLowerCase();
  return haystack.includes(query);
};

interface SearchFilteredViewProps {
  readonly children: ReactNode;
}

/**
 * Client-side search filter for the task table. Reads the active search query
 * from `TaskListContext`, narrows the current page's records to those whose
 * app / author / image substring matches, and re-publishes a filtered list
 * context for the children.
 *
 * When the query is empty this component is a no-op pass-through, so it's
 * safe to slot above any task list page.
 */
export const SearchFilteredView = ({ children }: SearchFilteredViewProps) => {
  const ctx = useListContext<Task>();
  const { state } = useTaskListContext();
  const query = state.searchQuery.trim().toLowerCase();

  const filteredCtx = useMemo(() => {
    if (!query) return ctx;
    const data = (ctx.data ?? []).filter(record => matchesQuery(record, query));
    return { ...ctx, data, total: data.length };
  }, [ctx, query]);

  if (!query) {
    return <>{children}</>;
  }

  return <ListContextProvider value={filteredCtx}>{children}</ListContextProvider>;
};
