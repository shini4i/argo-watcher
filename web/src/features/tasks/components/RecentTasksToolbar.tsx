import { useCallback, useMemo } from 'react';
import { Stack } from '@mui/material';
import { useListContext } from 'react-admin';
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
    toUrl: value => (value ? value : null),
    storage: true,
  },
  status: {
    fromUrl: raw => raw ?? null,
    toUrl: value => (value ? value : null),
    storage: false,
  },
};

/** Toolbar with status tabs, application filter, search, and the refresh control. */
export const RecentTasksToolbar = ({ storageKey = 'recentTasks' }: { storageKey?: string }) => {
  const { data, refetch } = useListContext<Task>();
  const records = useMemo(() => (Array.isArray(data) ? data : []), [data]);

  const { values, applied, apply } = useFilterState<RecentFiltersValues>({
    storageKey,
    schema: SCHEMA,
    defaults: DEFAULTS,
  });

  const { state: { searchQuery }, setSearchQuery } = useTaskListContext();

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

  const handleRefresh = useCallback(() => {
    const result = refetch?.();
    if (result && typeof (result as Promise<unknown>).catch === 'function') {
      (result as Promise<unknown>).catch(error => {
        if (import.meta.env.DEV) {
          console.warn('RecentTasksToolbar refresh failed', error);
        }
      });
    }
  }, [refetch]);

  const chips: FilterChipDescriptor[] = [];
  if (applied.app) {
    chips.push({
      key: 'app',
      labelPrefix: 'app',
      labelValue: applied.app,
      onRemove: () => apply({ ...values, app: '' }),
    });
  }

  const handleClearAll = useCallback(() => {
    apply({ app: '', status: null });
  }, [apply]);

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
              placeholder="Search app, author, image"
            />
            <RefreshControl onRefresh={handleRefresh} />
          </>
        }
      />
      <ActiveFilterBar chips={chips} onClearAll={chips.length > 0 ? handleClearAll : undefined} />
    </Stack>
  );
};
