import { Route } from 'react-router-dom';
import { Admin, CustomRoutes, Resource } from 'react-admin';
import { AppLayout } from './layout/AppLayout';
import { dataProvider } from './data/dataProvider';
import { authProvider } from './auth/authProvider';
import { AuthBootstrap } from './auth/AuthBootstrap';
import { LoginPage } from './auth/LoginPage';
import { RecentTasksList } from './features/tasks/RecentTasksList';
import { HistoryTasksList } from './features/tasks/HistoryTasksList';
import { AppNotification } from './layout/components/AppNotification';
import { TaskShow } from './features/tasks/show/TaskShow';
import { useThemeMode } from './theme';

/**
 * Root React-admin application wiring data/auth providers, resources, and custom routes.
 * Wrapped in AuthBootstrap to handle OAuth callbacks before routing.
 */
export const App = () => {
  const { theme } = useThemeMode();

  return (
    <AuthBootstrap>
      {keycloakEnabled => (
        <Admin
          layout={AppLayout}
          dataProvider={dataProvider}
          authProvider={authProvider}
          loginPage={keycloakEnabled ? LoginPage : false}
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
      )}
    </AuthBootstrap>
  );
};
