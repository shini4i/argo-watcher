import { Children, isValidElement, type ReactNode } from 'react';
import { Box, Stack } from '@mui/material';
import { List, Pagination, type ListProps } from 'react-admin';
import { PerPagePersistence, readPersistentPerPage } from '../../../shared/hooks/usePersistentPerPage';
import { TaskListProvider } from './TaskListContext';

interface TaskListLayoutProps {
  title?: string;
  perPageStorageKey: string;
  defaultPerPage?: number;
  header?: ReactNode | ReadonlyArray<ReactNode>;
  children: ReactNode;
  paginationOptions?: number[];
  listProps?: Partial<ListProps>;
  emptyComponent?: ReactNode | false;
}

/**
 * Shared wrapper for task list pages handling pagination persistence, headers, and empty states.
 */
/** Shared list scaffold wrapping React-admin's List with toolbar/header handling. */
export const TaskListLayout = ({
  title,
  perPageStorageKey,
  defaultPerPage = 25,
  header,
  children,
  paginationOptions = [10, 25, 50, 100],
  listProps,
  emptyComponent = false,
}: TaskListLayoutProps) => {
  const perPage = readPersistentPerPage(perPageStorageKey, defaultPerPage);

  const {
    pagination,
    resource = 'tasks',
    sort = { field: 'created', order: 'DESC' },
    actions = false,
    storeKey,
    ...rest
  } = listProps ?? {};

  const resolvedPagination = pagination ?? (
    <Pagination rowsPerPageOptions={paginationOptions} />
  );

  const headerContent = header
    ? Children.toArray(header).map(node =>
        typeof node === 'string' || typeof node === 'number' ? (
          <span key={`literal-${node}`}>{node}</span>
        ) : (
          node
        ),
      )
    : [];

  return (
    <TaskListProvider>
    <List
      title={title}
      resource={resource}
      sort={sort}
      perPage={perPage}
      pagination={resolvedPagination}
      actions={actions}
      storeKey={storeKey}
      empty={emptyComponent}
      {...rest}
    >
      <PerPagePersistence storageKey={perPageStorageKey} />
      <Stack sx={{ px: 2, py: 1, width: '100%' }} spacing={0.5}>
        {headerContent.length > 0 ? (
          headerContent.map(node => {
            const boxKey = isValidElement(node) && node.key != null ? String(node.key) : undefined;
            return (
              <Box key={boxKey} sx={{ width: '100%' }}>
                {node}
              </Box>
            );
          })
        ) : (
          <Box sx={{ width: '100%' }} />
        )}
      </Stack>
      {children}
    </List>
    </TaskListProvider>
  );
};
