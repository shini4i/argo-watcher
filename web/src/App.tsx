import { Route } from 'react-router-dom';
import { Admin, CustomRoutes, Resource } from 'react-admin';
import { AppLayout } from './layout/AppLayout';
import { dataProvider } from './data/dataProvider';
import { authProvider } from './auth/authProvider';
import { RecentTasksList } from './features/tasks/RecentTasksList';
import { HistoryTasksList } from './features/tasks/HistoryTasksList';
import { AppNotification } from './layout/components/AppNotification';
import { TaskShow } from './features/tasks/show/TaskShow';
import { useThemeMode } from './theme';

/**
 * Root React-admin application wiring data/auth providers, resources, and custom routes.
 */
export const App = () => {
  const { theme } = useThemeMode();

  return (
    <Admin
      layout={AppLayout}
      dataProvider={dataProvider}
      authProvider={authProvider}
      notification={AppNotification}
      disableTelemetry
      theme={theme}
    >
      <Resource name="tasks" options={{ label: 'Recent Tasks' }} list={RecentTasksList} />
      <CustomRoutes>
        <Route path="/history" element={<HistoryTasksList />} />
        <Route path="/task/:id" element={<TaskShow />} />
      </CustomRoutes>
    </Admin>
  );
};
