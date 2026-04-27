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
 * Client-side filter for the task table. Reads the active search query
 * from `TaskListContext`, narrows the *currently loaded page* to records
 * whose app / author / image substring matches, and re-publishes a
 * filtered list context for the children.
 *
 * Important: this is intentionally a page-scoped filter, not a search
 * across the entire backend. Callers (placeholder text, active-filter
 * chip) should reflect that scope so users do not assume a global search.
 *
 * When the query is empty this component is a no-op pass-through, so it
 * is safe to slot above any task list page.
 */
export const SearchFilteredView = ({ children }: SearchFilteredViewProps) => {
  const ctx = useListContext<Task>();
  const { state } = useTaskListContext();
  const query = state.searchQuery.trim().toLowerCase();
  const records = ctx.data;

  // react-admin produces a fresh context object reference every render, so
  // memoise only on the primitives we actually read for filtering. The
  // ctx spread below is cheap and stays correct across context updates.
  const filteredData = useMemo(() => {
    if (!query) return null;
    return (records ?? []).filter(record => matchesQuery(record, query));
  }, [records, query]);

  if (!query || !filteredData) {
    return <>{children}</>;
  }

  return (
    <ListContextProvider value={{ ...ctx, data: filteredData, total: filteredData.length }}>
      {children}
    </ListContextProvider>
  );
};
