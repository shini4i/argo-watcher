import { useEffect } from 'react';
import { render, screen } from '@testing-library/react';
import { ListContextProvider, useListContext } from 'react-admin';
import type { ListContextValue } from 'react-admin';
import { describe, expect, it } from 'vitest';
import type { Task } from '../../../data/types';
import { SearchFilteredView } from './SearchFilteredView';
import { TaskListProvider, useTaskListContext } from './TaskListContext';

const records: Task[] = [
  { id: '1', created: 1, updated: 2, app: 'checkout-api', author: 'alice', project: 'p', images: [{ image: 'app', tag: 'v1' }] },
  { id: '2', created: 3, updated: 4, app: 'payments', author: 'bob', project: 'p', images: [{ image: 'app', tag: 'v2' }] },
  { id: '3', created: 5, updated: 6, app: 'reports', author: 'carol', project: 'p', images: [{ image: 'cron', tag: 'v3' }] },
];

const Probe = () => {
  const ctx = useListContext<Task>();
  return (
    <ul data-testid="rows">
      {(ctx.data ?? []).map(record => (
        <li key={record.id}>{record.app}</li>
      ))}
    </ul>
  );
};

const QueryWriter = ({ query }: { query: string }) => {
  const { setSearchQuery } = useTaskListContext();
  useEffect(() => {
    setSearchQuery(query);
  }, [setSearchQuery, query]);
  return null;
};

const renderWith = (query: string) => {
  const baseCtx = {
    data: records,
    total: records.length,
  } as unknown as ListContextValue<Task>;

  return render(
    <TaskListProvider>
      <ListContextProvider value={baseCtx}>
        <QueryWriter query={query} />
        <SearchFilteredView>
          <Probe />
        </SearchFilteredView>
      </ListContextProvider>
    </TaskListProvider>,
  );
};

describe('SearchFilteredView', () => {
  it('passes through unchanged when the search query is empty', () => {
    renderWith('');
    expect(screen.getByTestId('rows').children).toHaveLength(3);
  });

  it('filters by app substring', () => {
    renderWith('payments');
    const items = screen.getByTestId('rows').children;
    expect(items).toHaveLength(1);
    expect(items[0].textContent).toBe('payments');
  });

  it('filters by author substring', () => {
    renderWith('alice');
    expect(screen.getByText('checkout-api')).toBeInTheDocument();
    expect(screen.queryByText('payments')).toBeNull();
  });

  it('filters by image:tag substring', () => {
    renderWith('cron:v3');
    expect(screen.getByText('reports')).toBeInTheDocument();
    expect(screen.queryByText('checkout-api')).toBeNull();
  });

  it('is case-insensitive', () => {
    renderWith('CHECKOUT');
    expect(screen.getByText('checkout-api')).toBeInTheDocument();
  });
});
