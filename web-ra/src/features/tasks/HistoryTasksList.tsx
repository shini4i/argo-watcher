import { useEffect } from 'react';
import { Card, CardContent, Typography } from '@mui/material';
import { Pagination, usePermissions } from 'react-admin';
import { TasksDatagrid } from './components/TasksDatagrid';
import { HistoryFilters } from './components/HistoryFilters';
import { HistoryExportMenu } from './components/HistoryExportMenu';
import { useKeycloakEnabled } from '../../shared/hooks/useKeycloakEnabled';
import { hasPrivilegedAccess } from '../../shared/utils';
import { TaskListLayout } from './components/TaskListLayout';

const STORAGE_KEY_PER_PAGE = 'historyTasks.perPage';

const EmptyState = () => (
  <Card variant="outlined">
    <CardContent>
      <Typography variant="h6">No tasks found</Typography>
      <Typography variant="body2" color="text.secondary">
        Adjust the date range or application filter to explore the archive.
      </Typography>
    </CardContent>
  </Card>
);

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
  const anonymizeForced = keycloakEnabled ? !hasPrivilegedAccess(groups, privilegedGroups) : false;

  return (
    <TaskListLayout
      perPageStorageKey={STORAGE_KEY_PER_PAGE}
      header={[
        <HistoryFilters key="filters" />,
        <HistoryExportMenu key="export-menu" anonymizeForced={anonymizeForced} />,
      ]}
      emptyComponent={<EmptyState />}
      listProps={{
        pagination: <Pagination rowsPerPageOptions={[10, 25, 50, 100]} />,
      }}
    >
      <TasksDatagrid />
    </TaskListLayout>
  );
};
