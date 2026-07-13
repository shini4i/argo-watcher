import { render, screen } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { TaskListLayout } from './TaskListLayout';

interface StubListContext {
  data: unknown[];
  total?: number;
  isPending?: boolean;
  filterValues?: Record<string, unknown>;
  error?: unknown;
}

const {
  listCalls,
  listContextRef,
  ListMock,
  PaginationMock,
  PerPagePersistenceMock,
} = vi.hoisted(() => {
  const listCallsInternal: Array<Record<string, unknown>> = [];
  const contextRef: { current: StubListContext } = { current: { data: [] } };

  const list = ({ children, ...props }: Record<string, unknown>) => {
    listCallsInternal.push(props);
    return <div data-testid="ra-list">{children}</div>;
  };

  const pagination = ({ rowsPerPageOptions }: { rowsPerPageOptions: number[] }) => (
    <div data-testid="ra-pagination" data-rows={rowsPerPageOptions.join(',')} />
  );

  const perPage = ({ storageKey }: { storageKey: string }) => (
    <div data-testid="per-page-persistence" data-storage={storageKey} />
  );

  return {
    listCalls: listCallsInternal,
    listContextRef: contextRef,
    ListMock: list,
    PaginationMock: pagination,
    PerPagePersistenceMock: perPage,
  };
});

vi.mock('react-admin', () => ({
  List: ListMock,
  Pagination: PaginationMock,
  // SearchFilteredView + TaskListLayout body both read useListContext; expose a
  // mutable stub so individual tests can drive the empty/populated branches.
  useListContext: () => listContextRef.current,
  ListContextProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

const readPersistentPerPageMock = vi.fn();

vi.mock('../../../shared/hooks/usePersistentPerPage', () => ({
  readPersistentPerPage: (...args: unknown[]) => readPersistentPerPageMock(...args),
  PerPagePersistence: PerPagePersistenceMock,
}));

describe('TaskListLayout', () => {
  beforeEach(() => {
    listCalls.length = 0;
    listContextRef.current = { data: [] };
    readPersistentPerPageMock.mockReset();
  });

  it('fills react-admin list props and renders header content when provided', () => {
    readPersistentPerPageMock.mockReturnValue(40);
    // Non-empty result so the body renders children rather than the empty state.
    listContextRef.current = { data: [{ id: 1 }], total: 1 };

    render(
      <TaskListLayout
        title="Recent Tasks"
        perPageStorageKey="recent.perPage"
        defaultPerPage={25}
        header={['Literal header', <button key="action">Custom Action</button>]}
        paginationOptions={[5, 10]}
        listProps={{
          resource: 'customTasks',
          sort: { field: 'author', order: 'ASC' },
          actions: <div data-testid="actions" />,
          pagination: <div data-testid="custom-pagination" />,
          storeKey: 'customStore',
        }}
        emptyComponent={<div data-testid="empty-state" />}
      >
        <div>Child content</div>
      </TaskListLayout>,
    );

    expect(readPersistentPerPageMock).toHaveBeenCalledWith('recent.perPage', 25);
    expect(screen.getByTestId('per-page-persistence')).toHaveAttribute('data-storage', 'recent.perPage');

    expect(screen.getByText('Literal header').tagName).toBe('SPAN');
    expect(screen.getByText('Custom Action')).toBeInTheDocument();
    expect(screen.getByText('Child content')).toBeInTheDocument();

    expect(listCalls).toHaveLength(1);
    const props = listCalls[0] as {
      title: string;
      resource: string;
      sort: Record<string, unknown>;
      perPage: number;
      pagination: ReactElement;
      actions: ReactElement;
      storeKey: string;
      empty: unknown;
    };

    expect(props.title).toBe('Recent Tasks');
    expect(props.resource).toBe('customTasks');
    expect(props.sort).toEqual({ field: 'author', order: 'ASC' });
    expect(props.perPage).toBe(40);
    expect(props.pagination.props['data-testid']).toBe('custom-pagination');
    expect(props.actions.props['data-testid']).toBe('actions');
    expect(props.storeKey).toBe('customStore');
    // The layout must NOT delegate the empty state to react-admin's <List empty>,
    // because react-admin renders it *instead of* the list, dropping the filter
    // toolbar. The empty placeholder is rendered inside the body instead.
    expect(props.empty).toBe(false);
  });

  it('renders the empty placeholder alongside the header when there are no rows and no filters', () => {
    readPersistentPerPageMock.mockReturnValue(25);
    listContextRef.current = { data: [], total: 0, isPending: false, filterValues: {} };

    render(
      <TaskListLayout
        perPageStorageKey="history.perPage"
        header={[<button key="filters">Date filter</button>]}
        emptyComponent={<div data-testid="empty-state" />}
      >
        <div data-testid="datagrid">Rows</div>
      </TaskListLayout>,
    );

    // The header (filters) stays mounted so the user can still pick a date range.
    expect(screen.getByText('Date filter')).toBeInTheDocument();
    expect(screen.getByTestId('empty-state')).toBeInTheDocument();
    // The datagrid children are replaced by the placeholder.
    expect(screen.queryByTestId('datagrid')).not.toBeInTheDocument();
  });

  it('keeps rendering the datagrid (not the layout placeholder) when filters are active', () => {
    readPersistentPerPageMock.mockReturnValue(25);
    listContextRef.current = {
      data: [],
      total: 0,
      isPending: false,
      filterValues: { start: 1, end: 2 },
    };

    render(
      <TaskListLayout
        perPageStorageKey="history.perPage"
        header={[<button key="filters">Date filter</button>]}
        emptyComponent={<div data-testid="empty-state" />}
      >
        <div data-testid="datagrid">Rows</div>
      </TaskListLayout>,
    );

    // With active filters, defer to the datagrid's own filtered empty state.
    expect(screen.getByText('Date filter')).toBeInTheDocument();
    expect(screen.getByTestId('datagrid')).toBeInTheDocument();
    expect(screen.queryByTestId('empty-state')).not.toBeInTheDocument();
  });

  it('defers to the datagrid (not the layout placeholder) when a fetch error leaves zero rows', () => {
    readPersistentPerPageMock.mockReturnValue(25);
    listContextRef.current = {
      data: [],
      total: 0,
      isPending: false,
      filterValues: {},
      error: new Error('backend unreachable'),
    };

    render(
      <TaskListLayout
        perPageStorageKey="history.perPage"
        header={[<button key="filters">Date filter</button>]}
        emptyComponent={<div data-testid="empty-state" />}
      >
        <div data-testid="datagrid">Rows</div>
      </TaskListLayout>,
    );

    // A backend error must not be misattributed to genuine emptiness.
    expect(screen.getByText('Date filter')).toBeInTheDocument();
    expect(screen.getByTestId('datagrid')).toBeInTheDocument();
    expect(screen.queryByTestId('empty-state')).not.toBeInTheDocument();
  });

  it('falls back to default pagination options and empty header placeholder', () => {
    readPersistentPerPageMock.mockReturnValue(15);
    listContextRef.current = { data: [{ id: 1 }], total: 1 };

    render(
      <TaskListLayout title="History" perPageStorageKey="history.perPage">
        <div>Row</div>
      </TaskListLayout>,
    );

    const props = listCalls.at(-1) as { perPage: number; pagination: ReactElement };
    expect(props.pagination.type).toBe(PaginationMock);
    expect(props.perPage).toBe(15);
    expect(props.pagination.props.rowsPerPageOptions).toEqual([10, 25, 50, 100]);
  });
});
