import { Children, isValidElement, type ReactNode } from 'react';
import { Box, Stack } from '@mui/material';
import { List, Pagination, useListContext, useRefresh, type ListProps } from 'react-admin';
import { describeReadFailure } from '../../../data/readFailure';
import { PerPagePersistence, readPersistentPerPage } from '../../../shared/hooks/usePersistentPerPage';
import { EmptyState, EmptyStateCta } from './EmptyState';
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
 * Gates inside <List>: <List empty> renders instead of the list, dropping the filter
 * toolbar. The error panel keys on absent rows — an errored query sets no `total`.
 */
const ListBody = ({
  emptyComponent,
  children,
}: {
  emptyComponent: ReactNode | false;
  children: ReactNode;
}) => {
  const { isPending, total, data, filterValues, error } = useListContext();
  const hasFilters = Object.keys(filterValues ?? {}).length > 0;
  // Retry has to reload every active query, not just the list: the toolbar's
  // status counts come from their own query, and a list-only refetch would
  // repopulate the grid while the pills stayed stuck on the failed fetch.
  const refresh = useRefresh();

  if (error && !isPending && !data?.length) {
    const failure = describeReadFailure(error);
    return (
      <EmptyState
        icon="error"
        title={failure.title}
        description={failure.detail}
        hint={failure.hint}
        cta={<EmptyStateCta label="Retry" onClick={refresh} />}
      />
    );
  }

  if (emptyComponent && !isPending && total === 0 && !hasFilters) {
    return <>{emptyComponent}</>;
  }

  return <>{children}</>;
};

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
      empty={false}
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
      <ListBody emptyComponent={emptyComponent}>{children}</ListBody>
    </List>
    </TaskListProvider>
  );
};
