import type { ReactNode } from 'react';
import { Box, Stack } from '@mui/material';
import { List, Pagination, type ListProps } from 'react-admin';
import { PerPagePersistence, readPersistentPerPage } from '../../../shared/hooks/usePersistentPerPage';

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

  const headerContent = Array.isArray(header) ? header : header ? [header] : [];

  return (
    <List
      {...(title !== undefined ? { title } : {})}
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
        <Stack
          direction={{ xs: 'column', md: 'row' }}
          spacing={{ xs: 1.5, md: 2 }}
          justifyContent="flex-end"
          alignItems={{ xs: 'flex-end', md: 'center' }}
        >
          {headerContent.length > 0 ? (
            headerContent.map((node, index) => (
              <Box
                key={index}
                sx={{
                  display: 'flex',
                  justifyContent: 'flex-end',
                  width: { xs: '100%', md: 'auto' },
                }}
              >
                {node}
              </Box>
            ))
          ) : (
            <Box sx={{ width: '100%' }} />
          )}
        </Stack>
      </Stack>
      {children}
    </List>
  );
};
