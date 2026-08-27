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
  refetch?: () => void;
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

const refreshMock = vi.fn();

vi.mock('react-admin', () => ({
  List: ListMock,
  Pagination: PaginationMock,
  useRefresh: () => refreshMock,
  // SearchFilteredView + TaskListLayout body both read useListContext.
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
    refreshMock.mockReset();
  });

  it('fills react-admin list props and renders header content when provided', () => {
    readPersistentPerPageMock.mockReturnValue(40);
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

    expect(screen.getByText('Date filter')).toBeInTheDocument();
    expect(screen.getByTestId('empty-state')).toBeInTheDocument();
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

    expect(screen.getByText('Date filter')).toBeInTheDocument();
    expect(screen.getByTestId('datagrid')).toBeInTheDocument();
    expect(screen.queryByTestId('empty-state')).not.toBeInTheDocument();
  });

  it('shows an honest error state (not the empty placeholder or datagrid) when a fetch fails', () => {
    readPersistentPerPageMock.mockReturnValue(25);
    const refetch = vi.fn();
    listContextRef.current = {
      data: [],
      total: 0,
      isPending: false,
      filterValues: {},
      error: new Error('backend unreachable'),
      refetch,
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

    expect(screen.getByText('Date filter')).toBeInTheDocument();
    expect(screen.getByText('Could not reach the Argo Watcher server')).toBeInTheDocument();
    expect(screen.queryByTestId('datagrid')).not.toBeInTheDocument();
    expect(screen.queryByTestId('empty-state')).not.toBeInTheDocument();

    // Retry must reload every active query: the toolbar's status counts come
    // from their own query, and a list-only refetch would leave them stuck.
    screen.getByRole('button', { name: 'Retry' }).click();
    expect(refreshMock).toHaveBeenCalledTimes(1);
  });

  // react-admin leaves `total` undefined on an errored query, so a gate on
  // `total === 0` fell through to the datagrid and read as an empty task list.
  it('shows the error state when a failed query reports no total at all', () => {
    readPersistentPerPageMock.mockReturnValue(25);
    listContextRef.current = {
      data: undefined as unknown as unknown[],
      total: undefined,
      isPending: false,
      filterValues: {},
      error: Object.assign(new Error('authentication provider unavailable'), {
        status: 503,
        body: { status: 'authentication provider unavailable', error: 'token validation failed' },
      }),
    };

    render(
      <TaskListLayout perPageStorageKey="history.perPage" emptyComponent={<div data-testid="empty-state" />}>
        <div data-testid="datagrid">Rows</div>
      </TaskListLayout>,
    );

    expect(screen.getByText('Argo Watcher cannot verify your session')).toBeInTheDocument();
    expect(screen.getByText(/could not reach the identity provider/)).toBeInTheDocument();
    // The remedy is the half that stops a pointless re-login, so pin it too.
    expect(screen.getByText(/OIDC issuer is reachable/)).toBeInTheDocument();
    expect(screen.queryByTestId('datagrid')).not.toBeInTheDocument();
    expect(screen.queryByTestId('empty-state')).not.toBeInTheDocument();
  });

  // History persists its filters, so a returning user hits an outage with filters set.
  // The outage must win over the datagrid's "adjust your filters" empty state.
  it('shows the error state even when filters are active', () => {
    readPersistentPerPageMock.mockReturnValue(25);
    listContextRef.current = {
      data: undefined as unknown as unknown[],
      total: undefined,
      isPending: false,
      filterValues: { app: 'demo', start: 1, end: 2 },
      error: Object.assign(new Error('authentication provider unavailable'), {
        status: 503,
        body: { status: 'authentication provider unavailable', error: 'token validation failed' },
      }),
    };

    render(
      <TaskListLayout perPageStorageKey="history.perPage" emptyComponent={<div data-testid="empty-state" />}>
        <div data-testid="datagrid">Rows</div>
      </TaskListLayout>,
    );

    expect(screen.getByText('Argo Watcher cannot verify your session')).toBeInTheDocument();
    expect(screen.queryByTestId('datagrid')).not.toBeInTheDocument();
  });

  it('keeps the populated grid (not the error panel) when a refetch fails with rows already loaded', () => {
    readPersistentPerPageMock.mockReturnValue(25);
    // react-admin keeps the previously loaded rows across a refetch, so a
    // transient auto-refresh error with total > 0 must not blank the grid.
    listContextRef.current = {
      data: [{ id: 1 }, { id: 2 }],
      total: 2,
      isPending: false,
      filterValues: {},
      error: new Error('transient refetch failure'),
      refetch: vi.fn(),
    };

    render(
      <TaskListLayout
        perPageStorageKey="history.perPage"
        emptyComponent={<div data-testid="empty-state" />}
      >
        <div data-testid="datagrid">Rows</div>
      </TaskListLayout>,
    );

    expect(screen.getByTestId('datagrid')).toBeInTheDocument();
    expect(screen.queryByText(/Could not reach/)).not.toBeInTheDocument();
    expect(screen.queryByTestId('empty-state')).not.toBeInTheDocument();
  });

  it('keeps showing the skeleton (defers to children) while a request is pending', () => {
    readPersistentPerPageMock.mockReturnValue(25);
    listContextRef.current = {
      data: [],
      total: 0,
      isPending: true,
      filterValues: {},
      error: new Error('stale error from previous attempt'),
    };

    render(
      <TaskListLayout
        perPageStorageKey="history.perPage"
        emptyComponent={<div data-testid="empty-state" />}
      >
        <div data-testid="datagrid">Rows</div>
      </TaskListLayout>,
    );

    expect(screen.getByTestId('datagrid')).toBeInTheDocument();
    expect(screen.queryByText(/Could not reach/)).not.toBeInTheDocument();
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
