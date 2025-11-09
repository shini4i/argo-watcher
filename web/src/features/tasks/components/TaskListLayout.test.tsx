import { render, screen } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { TaskListLayout } from './TaskListLayout';

const {
  listCalls,
  ListMock,
  PaginationMock,
  PerPagePersistenceMock,
} = vi.hoisted(() => {
  const listCallsInternal: Array<Record<string, unknown>> = [];

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
    ListMock: list,
    PaginationMock: pagination,
    PerPagePersistenceMock: perPage,
  };
});

vi.mock('react-admin', () => ({
  List: ListMock,
  Pagination: PaginationMock,
}));

const readPersistentPerPageMock = vi.fn();

vi.mock('../../../shared/hooks/usePersistentPerPage', () => ({
  readPersistentPerPage: (...args: unknown[]) => readPersistentPerPageMock(...args),
  PerPagePersistence: PerPagePersistenceMock,
}));

describe('TaskListLayout', () => {
  beforeEach(() => {
    listCalls.length = 0;
    readPersistentPerPageMock.mockReset();
  });

  it('fills react-admin list props and renders header content when provided', () => {
    readPersistentPerPageMock.mockReturnValue(40);

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
      empty: ReactElement;
    };

    expect(props.title).toBe('Recent Tasks');
    expect(props.resource).toBe('customTasks');
    expect(props.sort).toEqual({ field: 'author', order: 'ASC' });
    expect(props.perPage).toBe(40);
    expect(props.pagination.props['data-testid']).toBe('custom-pagination');
    expect(props.actions.props['data-testid']).toBe('actions');
    expect(props.storeKey).toBe('customStore');
    expect(props.empty.props['data-testid']).toBe('empty-state');
  });

  it('falls back to default pagination options and empty header placeholder', () => {
    readPersistentPerPageMock.mockReturnValue(15);

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
