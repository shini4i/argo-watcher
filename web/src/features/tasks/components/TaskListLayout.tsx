import { Children, isValidElement, type ReactNode } from 'react';
import { Box, Stack } from '@mui/material';
import { List, Pagination, useListContext, type ListProps } from 'react-admin';
import { PerPagePersistence, readPersistentPerPage } from '../../../shared/hooks/usePersistentPerPage';
import { EmptyState, EmptyStateCta } from './EmptyState';
import { SearchFilteredView } from './SearchFilteredView';
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
 * Renders the list body: the children (datagrid) normally, or the page-specific
 * empty placeholder when the backend returned zero rows and no filters are active.
 *
 * This gate lives here — inside <List> — rather than on <List empty>, because
 * react-admin renders the `empty` element *instead of* the entire list, which
 * drops the filter toolbar with it. Users would then land on an empty history
 * page with no way to widen the date range. Rendering the placeholder in the
 * body keeps the header/filters mounted above it. When filters are active we
 * defer to the datagrid's own filtered empty state (with its "Clear filters"
 * CTA), matching react-admin's original `shouldRenderEmptyPage` condition
 * (`!error && !isPending && total === 0 && !filterValues`).
 *
 * A fetch error (a 5xx, a network drop, or the request timing out — see
 * REQUEST_TIMEOUT_MS in httpClient) is rendered as an explicit error state with
 * a retry, NOT as the empty placeholder or the datagrid's "no tasks" message: a
 * load failure must never masquerade as genuine emptiness. The header/filters
 * stay mounted above it so the user can still adjust the query and retry.
 *
 * The error panel is gated on `total === 0` so it only replaces the body when
 * there is nothing to show. react-admin keeps the previously loaded rows across
 * a refetch, so a transient auto-refresh failure keeps the populated grid (the
 * error is surfaced via react-admin's notification) instead of blanking it.
 */
const ListBody = ({
  emptyComponent,
  children,
}: {
  emptyComponent: ReactNode | false;
  children: ReactNode;
}) => {
  const { isPending, total, filterValues, error, refetch } = useListContext();
  const hasFilters = Object.keys(filterValues ?? {}).length > 0;

  if (error && !isPending && total === 0) {
    return (
      <EmptyState
        icon="error"
        title="Couldn’t load tasks"
        description="The request failed or timed out. Check that the server is reachable, then try again."
        cta={<EmptyStateCta label="Retry" onClick={() => refetch()} />}
      />
    );
  }

  if (emptyComponent && !isPending && total === 0 && !hasFilters) {
    return <>{emptyComponent}</>;
  }

  return <>{children}</>;
};

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
      <ListBody emptyComponent={emptyComponent}>
        <SearchFilteredView>{children}</SearchFilteredView>
      </ListBody>
    </List>
    </TaskListProvider>
  );
};
