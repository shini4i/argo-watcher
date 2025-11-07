import { useEffect } from 'react';
import { Card, CardContent, Typography } from '@mui/material';
import { Pagination } from 'react-admin';
import { TasksDatagrid } from './components/TasksDatagrid';
import { RecentTasksToolbar } from './components/RecentTasksToolbar';
import { TaskListLayout } from './components/TaskListLayout';

const STORAGE_KEY_PER_PAGE = 'recentTasks.perPage';
const DEFAULT_PER_PAGE = 25;

const EmptyState = () => (
  <Card variant="outlined">
    <CardContent>
      <Typography variant="h6">No recent tasks</Typography>
      <Typography variant="body2" color="text.secondary">
        Once deployments are triggered they will appear here automatically.
      </Typography>
    </CardContent>
  </Card>
);

/**
 * Recent tasks list routed at `/` providing sortable columns, pagination, application filtering, and auto-refresh controls.
 */
export const RecentTasksList = () => {
  useEffect(() => {
    const previousTitle = document.title;
    document.title = 'Recent Tasks — Argo Watcher';
    return () => {
      document.title = previousTitle;
    };
  }, []);

  return (
    <TaskListLayout
      perPageStorageKey={STORAGE_KEY_PER_PAGE}
      defaultPerPage={DEFAULT_PER_PAGE}
      header={<RecentTasksToolbar storageKey="recentTasks.app" />}
      emptyComponent={<EmptyState />}
      listProps={{
        storeKey: 'recentTasks',
        pagination: <Pagination rowsPerPageOptions={[10, 25, 50, 100]} />,
      }}
    >
      <TasksDatagrid />
    </TaskListLayout>
  );
};
