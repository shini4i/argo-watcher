import { useEffect } from 'react';
import { Pagination, usePermissions } from 'react-admin';
import { TasksDatagrid } from './components/TasksDatagrid';
import { HistoryFilters } from './components/HistoryFilters';
import { HistoryExportMenu } from './components/HistoryExportMenu';
import { useKeycloakEnabled } from '../../shared/hooks/useKeycloakEnabled';
import { hasPrivilegedAccess } from '../../shared/utils';
import { TaskListLayout } from './components/TaskListLayout';
import { NoTasksPlaceholder } from './components/NoTasksPlaceholder';

const STORAGE_KEY_PER_PAGE = 'historyTasks.perPage';

/**
 * History tasks list rendering archival deployments with advanced filters and export actions.
 */
/** React-admin list page surfacing historical tasks with filters and exports. */
export const HistoryTasksList = () => {
  useEffect(() => {
    const previousTitle = document.title;
    document.title = 'History Tasks — Argo Watcher';
    return () => {
      document.title = previousTitle;
    };
  }, []);
  const keycloakEnabled = useKeycloakEnabled();
  const { permissions } = usePermissions();
  const groups: readonly string[] = (permissions as { groups?: string[] })?.groups ?? [];
  const privilegedGroups: readonly string[] = (permissions as { privilegedGroups?: string[] })?.privilegedGroups ?? [];
  const userIsPrivileged = hasPrivilegedAccess(groups, privilegedGroups);
  const anonymizeForced = keycloakEnabled ? !userIsPrivileged : false;
  const exportEnabled = keycloakEnabled ? userIsPrivileged : true;

  return (
    <TaskListLayout
      perPageStorageKey={STORAGE_KEY_PER_PAGE}
      header={[
        <HistoryFilters key="filters" />,
        exportEnabled ? (
          <HistoryExportMenu key="export-menu" anonymizeForced={anonymizeForced} />
        ) : null,
      ]}
      emptyComponent={
        <NoTasksPlaceholder
          title="No history yet"
          description="Adjust filters or wait for past deployments to sync."
        />
      }
      listProps={{
        storeKey: 'historyTasks',
        pagination: <Pagination rowsPerPageOptions={[10, 25, 50, 100]} />,
      }}
    >
      <TasksDatagrid />
    </TaskListLayout>
  );
};
