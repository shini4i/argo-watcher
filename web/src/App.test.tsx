import { render, screen } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from './App';
import { AppLayout } from './layout/AppLayout';

const {
  adminCalls,
  resourceCalls,
  routeCalls,
  MockAdmin,
  MockResource,
  MockCustomRoutes,
  MockRoute,
  RecentTasksListStub,
  HistoryTasksListStub,
  TaskShowStub,
  AppNotificationStub,
  dataProviderStub,
  authProviderStub,
} = vi.hoisted(() => {
  const admin: Array<Record<string, unknown>> = [];
  const resource: Array<Record<string, unknown>> = [];
  const route: Array<Record<string, unknown>> = [];

  const Admin = ({ children, ...props }: { children: ReactElement }) => {
    admin.push(props);
    return (
      <div data-testid="admin-shell">
        {children}
      </div>
    );
  };

  const Resource = (props: Record<string, unknown>) => {
    resource.push(props);
    return <div data-testid={`resource-${props.name as string}`} />;
  };

  const CustomRoutes = ({ children }: { children: ReactElement }) => (
    <div data-testid="custom-routes">{children}</div>
  );

  const Route = (props: Record<string, unknown>) => {
    route.push(props);
    return <div data-testid={`route-${props.path as string}`} />;
  };

  const RecentList = () => <div data-testid="recent-list" />;
  const HistoryList = () => <div data-testid="history-list" />;
  const TaskShow = () => <div data-testid="task-show" />;
  const AppNotification = () => <div data-testid="app-notification" />;
  const dataProvider = { id: 'data-provider-stub' };
  const authProvider = { id: 'auth-provider-stub' };

  return {
    adminCalls: admin,
    resourceCalls: resource,
    routeCalls: route,
    MockAdmin: Admin,
    MockResource: Resource,
    MockCustomRoutes: CustomRoutes,
    MockRoute: Route,
    RecentTasksListStub: RecentList,
    HistoryTasksListStub: HistoryList,
    TaskShowStub: TaskShow,
    AppNotificationStub: AppNotification,
    dataProviderStub: dataProvider,
    authProviderStub: authProvider,
  };
});

vi.mock('react-admin', () => ({
  Admin: MockAdmin,
  Resource: MockResource,
  CustomRoutes: MockCustomRoutes,
}));

vi.mock('react-router-dom', () => ({
  Route: MockRoute,
}));

vi.mock('./data/dataProvider', () => ({ dataProvider: dataProviderStub }));
vi.mock('./auth/authProvider', () => ({ authProvider: authProviderStub }));
vi.mock('./features/tasks/RecentTasksList', () => ({ RecentTasksList: RecentTasksListStub }));
vi.mock('./features/tasks/HistoryTasksList', () => ({ HistoryTasksList: HistoryTasksListStub }));
vi.mock('./features/tasks/show/TaskShow', () => ({ TaskShow: TaskShowStub }));
vi.mock('./layout/components/AppNotification', () => ({ AppNotification: AppNotificationStub }));
vi.mock('./theme', () => ({ useThemeMode: () => ({ theme: { paletteMode: 'dark' } }) }));

describe('App', () => {
  beforeEach(() => {
    adminCalls.length = 0;
    resourceCalls.length = 0;
    routeCalls.length = 0;
  });

  it('wires Admin with layout, providers, resources, and custom routes', () => {
    render(<App />);

    expect(screen.getByTestId('admin-shell')).toBeInTheDocument();
    expect(adminCalls).toHaveLength(1);
    const adminProps = adminCalls[0];
    expect(adminProps.layout).toBe(AppLayout);
    expect(adminProps.dataProvider).toBe(dataProviderStub);
    expect(adminProps.authProvider).toBe(authProviderStub);
    expect(adminProps.notification).toBe(AppNotificationStub);
    expect(adminProps.disableTelemetry).toBe(true);
    expect(adminProps.theme).toEqual({ paletteMode: 'dark' });

    expect(resourceCalls).toHaveLength(1);
    expect(resourceCalls[0].name).toBe('tasks');
    expect(resourceCalls[0].list).toBe(RecentTasksListStub);

    expect(routeCalls.map(props => props.path)).toEqual(['/history', '/task/:id']);

    const historyRoute = routeCalls.find(props => props.path === '/history');
    expect((historyRoute?.element as ReactElement)?.type).toBe(HistoryTasksListStub);

    const taskRoute = routeCalls.find(props => props.path === '/task/:id');
    expect((taskRoute?.element as ReactElement)?.type).toBe(TaskShowStub);
  });
});
