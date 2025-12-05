import { useEffect } from 'react';
import { Pagination } from 'react-admin';
import { TasksDatagrid } from './components/TasksDatagrid';
import { HistoryFilters } from './components/HistoryFilters';
import { TaskListLayout } from './components/TaskListLayout';
import { NoTasksPlaceholder } from './components/NoTasksPlaceholder';

const STORAGE_KEY_PER_PAGE = 'historyTasks.perPage';

/**
 * React-admin list page surfacing historical tasks with filters.
 */
export const HistoryTasksList = () => {
  useEffect(() => {
    const previousTitle = document.title;
    document.title = 'History Tasks — Argo Watcher';
    return () => {
      document.title = previousTitle;
    };
  }, []);

  return (
    <TaskListLayout
      perPageStorageKey={STORAGE_KEY_PER_PAGE}
      header={[<HistoryFilters key="filters" />]}
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
