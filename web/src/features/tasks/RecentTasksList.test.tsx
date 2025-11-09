import { render } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { RecentTasksList } from './RecentTasksList';

const {
  layoutCalls,
  TaskListLayoutMock,
  RecentTasksToolbarMock,
  NoTasksPlaceholderMock,
  TasksDatagridMock,
  PaginationMock,
} = vi.hoisted(() => {
  const layoutCallsInternal: Array<Record<string, unknown>> = [];

  const taskListLayout = (props: Record<string, unknown>) => {
    layoutCallsInternal.push(props);
    return <div data-testid="recent-task-layout">{props.children}</div>;
  };

  const toolbar = ({ storageKey }: { storageKey: string }) => (
    <div data-testid="recent-toolbar" data-storage={storageKey} />
  );

  const placeholder = (props: Record<string, unknown>) => (
    <div data-testid="recent-placeholder" {...props} />
  );

  const datagrid = () => <div data-testid="recent-datagrid" />;

  const pagination = ({ rowsPerPageOptions }: { rowsPerPageOptions: number[] }) => (
    <div data-testid="recent-pagination" data-rows={rowsPerPageOptions.join(',')} />
  );

  return {
    layoutCalls: layoutCallsInternal,
    TaskListLayoutMock: taskListLayout,
    RecentTasksToolbarMock: toolbar,
    NoTasksPlaceholderMock: placeholder,
    TasksDatagridMock: datagrid,
    PaginationMock: pagination,
  };
});

vi.mock('react-admin', () => ({
  Pagination: PaginationMock,
}));

vi.mock('./components/TaskListLayout', () => ({
  TaskListLayout: TaskListLayoutMock,
}));

vi.mock('./components/RecentTasksToolbar', () => ({
  RecentTasksToolbar: RecentTasksToolbarMock,
}));

vi.mock('./components/NoTasksPlaceholder', () => ({
  NoTasksPlaceholder: NoTasksPlaceholderMock,
}));

vi.mock('./components/TasksDatagrid', () => ({
  TasksDatagrid: TasksDatagridMock,
}));

const lastLayoutProps = () => layoutCalls.at(-1)!;

describe('RecentTasksList', () => {
  beforeEach(() => {
    layoutCalls.length = 0;
    document.title = 'Original recent title';
  });

  it('configures the list layout, toolbar, and placeholder', () => {
    const { unmount } = render(<RecentTasksList />);

    expect(document.title).toBe('Recent Tasks — Argo Watcher');

    const props = lastLayoutProps() as {
      perPageStorageKey: string;
      defaultPerPage: number;
      header: ReactElement;
      listProps: { pagination?: ReactElement; storeKey?: string };
      emptyComponent: ReactElement;
    };

    expect(props.perPageStorageKey).toBe('recentTasks.perPage');
    expect(props.defaultPerPage).toBe(25);

    const headerNode = props.header;
    expect(headerNode.type).toBe(RecentTasksToolbarMock);
    expect(headerNode.props.storageKey).toBe('recentTasks.app');

    const paginationElement = props.listProps.pagination!;
    expect(paginationElement.props.rowsPerPageOptions).toEqual([10, 25, 50, 100]);
    expect(props.listProps.storeKey).toBe('recentTasks');

    const emptyComponent = props.emptyComponent;
    expect(emptyComponent.props.title).toMatch(/No recent tasks/i);
    expect(emptyComponent.props.description).toMatch(/Kick off a deployment/i);

    unmount();
    expect(document.title).toBe('Original recent title');
  });
});
