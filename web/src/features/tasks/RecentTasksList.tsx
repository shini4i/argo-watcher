import { useEffect } from 'react';
import { Pagination } from 'react-admin';
import { TasksDatagrid } from './components/TasksDatagrid';
import { RecentTasksToolbar } from './components/RecentTasksToolbar';
import { TaskListLayout } from './components/TaskListLayout';
import { NoTasksPlaceholder } from './components/NoTasksPlaceholder';

const STORAGE_KEY_PER_PAGE = 'recentTasks.perPage';
const DEFAULT_PER_PAGE = 25;

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
      emptyComponent={
        <NoTasksPlaceholder
          title="No recent tasks so far…"
          description="Kick off a deployment and we’ll list it here automatically."
        />
      }
      listProps={{
        storeKey: 'recentTasks',
        pagination: <Pagination rowsPerPageOptions={[10, 25, 50, 100]} />,
      }}
    >
      <TasksDatagrid />
    </TaskListLayout>
  );
};
