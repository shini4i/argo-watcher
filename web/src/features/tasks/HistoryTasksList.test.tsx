import { render } from '@testing-library/react';
import { Children, type ReactElement } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { HistoryTasksList } from './HistoryTasksList';

let permissionsMock: Record<string, unknown> = {};
let keycloakEnabled = false;

const {
  layoutCalls,
  PaginationMock,
  TaskListLayoutMock,
  HistoryFiltersMock,
  HistoryExportMenuMock,
  NoTasksPlaceholderMock,
  TasksDatagridMock,
  hasPrivilegedAccessMock,
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
  const exportMenu = ({ anonymizeForced }: { anonymizeForced: boolean }) => (
    <div data-testid="history-export" data-anonymize={String(anonymizeForced)} />
  );
  const placeholder = (props: Record<string, unknown>) => (
    <div data-testid="history-placeholder" {...props} />
  );
  const datagrid = () => <div data-testid="history-datagrid" />;

  const privilegedAccess = vi.fn(() => false);

  return {
    layoutCalls: layoutCallsInternal,
    PaginationMock: pagination,
    TaskListLayoutMock: taskListLayout,
    HistoryFiltersMock: filters,
    HistoryExportMenuMock: exportMenu,
    NoTasksPlaceholderMock: placeholder,
    TasksDatagridMock: datagrid,
    hasPrivilegedAccessMock: privilegedAccess,
  };
});

vi.mock('react-admin', () => ({
  Pagination: PaginationMock,
  usePermissions: () => ({ permissions: permissionsMock }),
}));

vi.mock('./components/TaskListLayout', () => ({
  TaskListLayout: TaskListLayoutMock,
}));

vi.mock('./components/HistoryFilters', () => ({
  HistoryFilters: HistoryFiltersMock,
}));

vi.mock('./components/HistoryExportMenu', () => ({
  HistoryExportMenu: HistoryExportMenuMock,
}));

vi.mock('./components/NoTasksPlaceholder', () => ({
  NoTasksPlaceholder: NoTasksPlaceholderMock,
}));

vi.mock('./components/TasksDatagrid', () => ({
  TasksDatagrid: TasksDatagridMock,
}));

vi.mock('../../shared/hooks/useKeycloakEnabled', () => ({
  useKeycloakEnabled: () => keycloakEnabled,
}));

vi.mock('../../shared/utils', () => ({
  hasPrivilegedAccess: hasPrivilegedAccessMock,
}));

const lastLayoutProps = () => layoutCalls.at(-1)!;

describe('HistoryTasksList', () => {
  beforeEach(() => {
    layoutCalls.length = 0;
    permissionsMock = { groups: ['dev'], privilegedGroups: ['ops'] };
    keycloakEnabled = false;
    hasPrivilegedAccessMock.mockReturnValue(true);
    document.title = 'Original title';
  });

  it('renders filters, export menu, and placeholder when exports are allowed', () => {
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

    const headerNodes = Children.toArray(props.header).filter(Boolean) as ReactElement[];
    expect(headerNodes.map(node => node.type)).toContain(HistoryFiltersMock);

    const exportNode = headerNodes.find(node => node.type === HistoryExportMenuMock) as ReactElement;
    expect(exportNode).toBeDefined();
    expect(exportNode.props.anonymizeForced).toBe(false);

    const emptyComponent = props.emptyComponent as ReactElement;
    expect(emptyComponent.props.title).toBe('No history yet');
    expect(emptyComponent.props.description).toMatch(/Adjust filters/);

    const paginationElement = props.listProps.pagination!;
    expect(paginationElement.props.rowsPerPageOptions).toEqual([10, 25, 50, 100]);

    unmount();
    expect(document.title).toBe('Original title');
  });

  it('hides the export menu when Keycloak is enabled and user lacks privileges', () => {
    keycloakEnabled = true;
    hasPrivilegedAccessMock.mockReturnValue(false);

    render(<HistoryTasksList />);

    const props = lastLayoutProps() as { header: ReactElement | ReactElement[] };
    const headerNodes = Children.toArray(props.header).filter(Boolean) as ReactElement[];
    expect(headerNodes).toHaveLength(1);
    expect(headerNodes[0].type).toBe(HistoryFiltersMock);
  });
});
