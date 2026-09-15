import { useEffect } from 'react';
import { Box, Stack } from '@mui/material';
import { Pagination, useGetIdentity } from 'react-admin';
import { TasksDatagrid } from './components/TasksDatagrid';
import { RecentTasksToolbar } from './components/RecentTasksToolbar';
import { TaskListLayout } from './components/TaskListLayout';
import { EmptyState } from './components/EmptyState';
import { ShortcutHint } from './components/ShortcutHint';

const STORAGE_KEY_PER_PAGE = 'recentTasks.perPage';
const DEFAULT_PER_PAGE = 25;

/** The hint shares the pagination row rather than adding a band of its own. */
const RecentPagination = () => {
  const { data: identity } = useGetIdentity();

  return (
    <Stack
      direction={{ xs: 'column', sm: 'row' }}
      sx={{ alignItems: 'center', justifyContent: 'space-between', px: 2, width: '100%' }}
    >
      <ShortcutHint showScope={Boolean(identity?.email)} />
      <Box>
        <Pagination rowsPerPageOptions={[10, 25, 50, 100]} />
      </Box>
    </Stack>
  );
};

/** Routed at `/`. */
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
      header={<RecentTasksToolbar storageKey="recentTasks" />}
      emptyComponent={
        <EmptyState
          icon="inbox"
          title="No recent tasks so far…"
          description="Kick off a deployment and we’ll list it here automatically."
        />
      }
      listProps={{
        storeKey: 'recentTasks',
        pagination: <RecentPagination />,
      }}
    >
      <TasksDatagrid />
    </TaskListLayout>
  );
};
