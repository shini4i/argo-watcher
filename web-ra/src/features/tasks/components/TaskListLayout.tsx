import type { ReactNode } from 'react';
import { Box, Card, CardContent, Stack, Typography } from '@mui/material';
import { List, Pagination, type ListProps } from 'react-admin';
import { PerPagePersistence, readPersistentPerPage } from '../../../shared/hooks/usePersistentPerPage';

interface TaskListLayoutProps {
  title?: string;
  perPageStorageKey: string;
  defaultPerPage?: number;
  header?: ReactNode | ReadonlyArray<ReactNode>;
  children: ReactNode;
  emptyComponent?: ReactNode;
  paginationOptions?: number[];
  listProps?: Partial<ListProps>;
}

const defaultEmptyState = (
  <Card variant="outlined">
    <CardContent>
      <Typography variant="h6">No tasks found</Typography>
      <Typography variant="body2" color="text.secondary">
        No tasks match the current filters.
      </Typography>
    </CardContent>
  </Card>
);

export const TaskListLayout = ({
  title,
  perPageStorageKey,
  defaultPerPage = 25,
  header,
  children,
  emptyComponent = defaultEmptyState,
  paginationOptions = [10, 25, 50, 100],
  listProps,
}: TaskListLayoutProps) => {
  const perPage = readPersistentPerPage(perPageStorageKey, defaultPerPage);

  const { pagination, resource = 'tasks', sort = { field: 'created', order: 'DESC' }, actions = false, ...rest } =
    listProps ?? {};

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
      empty={emptyComponent}
      pagination={resolvedPagination}
      actions={actions}
      {...rest}
    >
      <PerPagePersistence storageKey={perPageStorageKey} />
      <Stack
        direction={{ xs: 'column', md: 'row' }}
        spacing={{ xs: 1.5, md: 2 }}
        sx={{ px: 2, py: 1, width: '100%' }}
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
      {children}
    </List>
  );
};
