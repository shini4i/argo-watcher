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
 * Narrows the *currently loaded page*, not the whole backend, and that scope is
 * intentional. Callers (placeholder text, active-filter chip) should reflect it
 * so users do not assume a global search.
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
