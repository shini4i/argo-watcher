import { useCallback, useEffect, useMemo, useRef } from 'react';
import { Stack } from '@mui/material';
import { useListContext, useRefresh } from 'react-admin';
import {
  ApplicationFilter,
  normalizeApplicationFilterValue,
} from './ApplicationFilter';
import type { Task } from '../../../data/types';
import { useFilterState, type FilterStateSchema } from '../../../shared/hooks/useFilterState';
import { ActiveFilterBar, type FilterChipDescriptor } from './ActiveFilterBar';
import { ListToolbar } from './ListToolbar';
import { RefreshControl } from './RefreshControl';
import { SearchInput } from './SearchInput';
import { StatusTabs } from './StatusTabs';
import { useTaskListContext } from './TaskListContext';

interface RecentFiltersValues extends Record<string, unknown> {
  app: string;
  status: string | null;
}

const DEFAULTS: RecentFiltersValues = { app: '', status: null };

const SCHEMA: FilterStateSchema<RecentFiltersValues> = {
  app: {
    fromUrl: raw => normalizeApplicationFilterValue(raw),
    toUrl: value => value || null,
    storage: true,
  },
  status: {
    fromUrl: raw => raw ?? null,
    toUrl: value => value || null,
    storage: false,
  },
};

export const RecentTasksToolbar = ({ storageKey = 'recentTasks' }: { storageKey?: string }) => {
  const { data } = useListContext<Task>();
  const records = useMemo(() => (Array.isArray(data) ? data : []), [data]);
  // Refresh every active query, not just the list: the status pills are backed
  // by their own useGetList, so the list's `refetch` would leave their counts
  // frozen at whatever the first load saw.
  const handleRefresh = useRefresh();

  const { values, applied, apply } = useFilterState<RecentFiltersValues>({
    storageKey,
    schema: SCHEMA,
    defaults: DEFAULTS,
  });

  const { state: { searchQuery }, setSearchQuery, registerClearAll } = useTaskListContext();

  const handleApplicationChange = useCallback(
    (next: string) => {
      apply({ ...values, app: normalizeApplicationFilterValue(next) });
    },
    [apply, values],
  );

  const handleStatusChange = useCallback(
    (next: string | null) => {
      apply({ ...values, status: next });
    },
    [apply, values],
  );

  const chips: FilterChipDescriptor[] = [];
  if (applied.app) {
    chips.push({
      key: 'app',
      labelPrefix: 'app',
      labelValue: applied.app,
      onRemove: () => apply({ ...values, app: '' }),
    });
  }
  if (searchQuery) {
    chips.push({
      key: 'search',
      labelPrefix: 'search',
      labelValue: searchQuery,
      onRemove: () => setSearchQuery(''),
    });
  }

  const handleClearAll = useCallback(() => {
    apply({ app: '', status: null });
    setSearchQuery('');
  }, [apply, setSearchQuery]);

  // `apply` re-identifies on every searchParams/filterValues change, so
  // re-registering the handler each render would thrash the context ref and
  // briefly leave Datagrid's "Clear filters" CTA pointing at null. Park the
  // latest handler in a ref and register a stable indirector exactly once.
  const clearAllHandlerRef = useRef(handleClearAll);
  useEffect(() => {
    clearAllHandlerRef.current = handleClearAll;
  });
  useEffect(
    () => registerClearAll(() => clearAllHandlerRef.current()),
    [registerClearAll],
  );

  return (
    <Stack spacing={0.5} sx={{ width: '100%' }}>
      <ListToolbar
        left={<StatusTabs value={applied.status} onChange={handleStatusChange} />}
        right={
          <>
            <ApplicationFilter
              storageKey={`${storageKey}.app`}
              records={records}
              value={applied.app}
              onChange={handleApplicationChange}
            />
            <SearchInput
              value={searchQuery}
              onChange={setSearchQuery}
              placeholder="Filter loaded rows…"
            />
            <RefreshControl onRefresh={handleRefresh} />
          </>
        }
      />
      <ActiveFilterBar chips={chips} onClearAll={chips.length > 0 ? handleClearAll : undefined} />
    </Stack>
  );
};
