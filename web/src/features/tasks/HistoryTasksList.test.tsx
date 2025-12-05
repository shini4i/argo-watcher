import { render } from '@testing-library/react';
import { Children, type ReactElement, type ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { HistoryTasksList } from './HistoryTasksList';

const {
  layoutCalls,
  PaginationMock,
  TaskListLayoutMock,
  HistoryFiltersMock,
  NoTasksPlaceholderMock,
  TasksDatagridMock,
} = vi.hoisted(() => {
  const layoutCallsInternal: Array<Record<string, unknown>> = [];

  const pagination = ({ rowsPerPageOptions }: { rowsPerPageOptions: number[] }) => (
    <div data-testid="history-pagination" data-rows={rowsPerPageOptions.join(',')} />
  );

  const taskListLayout = (props: Record<string, unknown>) => {
    layoutCallsInternal.push(props);
    return <div data-testid="history-task-layout">{props.children}</div>;
  };

  const filters = () => <div data-testid="history-filters" />;
  const placeholder = (props: Record<string, unknown>) => (
    <div data-testid="history-placeholder" {...props} />
  );
  const datagrid = () => <div data-testid="history-datagrid" />;

  return {
    layoutCalls: layoutCallsInternal,
    PaginationMock: pagination,
    TaskListLayoutMock: taskListLayout,
    HistoryFiltersMock: filters,
    NoTasksPlaceholderMock: placeholder,
    TasksDatagridMock: datagrid,
  };
});

vi.mock('react-admin', () => ({
  Pagination: PaginationMock,
}));

vi.mock('./components/TaskListLayout', () => ({
  TaskListLayout: TaskListLayoutMock,
}));

vi.mock('./components/HistoryFilters', () => ({
  HistoryFilters: HistoryFiltersMock,
}));

vi.mock('./components/NoTasksPlaceholder', () => ({
  NoTasksPlaceholder: NoTasksPlaceholderMock,
}));

vi.mock('./components/TasksDatagrid', () => ({
  TasksDatagrid: TasksDatagridMock,
}));

const lastLayoutProps = () => layoutCalls.at(-1)!;

/** Type guard ensuring a node is a concrete ReactElement before use in expectations. */
const isReactElement = (node: ReactNode): node is ReactElement =>
  typeof node === 'object' && node !== null && 'type' in (node as Record<string, unknown>);

describe('HistoryTasksList', () => {
  beforeEach(() => {
    layoutCalls.length = 0;
    document.title = 'Original title';
  });

  it('renders filters and placeholder', () => {
    const { unmount } = render(<HistoryTasksList />);

    expect(document.title).toBe('History Tasks — Argo Watcher');

    const props = lastLayoutProps() as {
      perPageStorageKey: string;
      header: ReactElement | ReactElement[];
      listProps: { storeKey?: string; pagination?: ReactElement };
      emptyComponent: ReactElement;
    };
    expect(props.perPageStorageKey).toBe('historyTasks.perPage');
    expect(props.listProps).toMatchObject({
      storeKey: 'historyTasks',
    });

    const headerNodes = Children.toArray(props.header).filter(isReactElement);
    expect(headerNodes.map(node => node.type)).toContain(HistoryFiltersMock);
    expect(headerNodes).toHaveLength(1);

    const emptyComponent = props.emptyComponent;
    expect(emptyComponent.props.title).toBe('No history yet');
    expect(emptyComponent.props.description).toMatch(/Adjust filters/);

    const paginationElement = props.listProps.pagination!;
    expect(paginationElement.props.rowsPerPageOptions).toEqual([10, 25, 50, 100]);

    unmount();
    expect(document.title).toBe('Original title');
  });
});
