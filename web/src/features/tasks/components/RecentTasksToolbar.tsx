import { useCallback, useEffect, useMemo, useState } from 'react';
import { Stack } from '@mui/material';
import { useListContext } from 'react-admin';
import { useSearchParams } from 'react-router-dom';
import {
  ApplicationFilter,
  normalizeApplicationFilterValue,
  readInitialApplication,
} from './ApplicationFilter';
import type { Task } from '../../../data/types';
import { getBrowserWindow } from '../../../shared/utils';
import { ActiveFilterBar, type FilterChipDescriptor } from './ActiveFilterBar';
import { ListToolbar } from './ListToolbar';
import { RefreshControl } from './RefreshControl';
import { SearchInput } from './SearchInput';
import { StatusTabs } from './StatusTabs';

/** Toolbar with status tabs, application filter, search, and the refresh control. */
export const RecentTasksToolbar = ({ storageKey = 'recentTasks.app' }: { storageKey?: string }) => {
  const { data, filterValues = {}, setFilters, refetch } = useListContext<Task>();
  const records = useMemo(() => (Array.isArray(data) ? data : []), [data]);

  const [searchParams, setSearchParams] = useSearchParams();
  const searchParamsKey = searchParams.toString();

  const filterAppValue = typeof filterValues?.app === 'string' ? (filterValues.app as string) : '';
  const filterStatusValue =
    typeof filterValues?.status === 'string' ? (filterValues.status as string) : null;

  const normalizedFilterApplication = useMemo(
    () => normalizeApplicationFilterValue(filterAppValue),
    [filterAppValue],
  );
  const normalizedQueryApplication = useMemo(
    () => normalizeApplicationFilterValue(searchParams.get('app')),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [searchParamsKey],
  );

  const [application, setApplication] = useState<string>(() => {
    if (normalizedQueryApplication) return normalizedQueryApplication;
    if (normalizedFilterApplication) return normalizedFilterApplication;
    return readInitialApplication(storageKey);
  });
  const normalizedApplication = normalizeApplicationFilterValue(application);

  const [search, setSearch] = useState('');

  useEffect(() => {
    const hasFilterMismatch = filterAppValue !== normalizedApplication;
    if (hasFilterMismatch && setFilters) {
      const nextFilters: Record<string, unknown> = { ...filterValues };
      if (normalizedApplication) {
        nextFilters.app = normalizedApplication;
      } else {
        delete nextFilters.app;
      }
      setFilters(nextFilters, {}, false);
    }

    if (normalizedQueryApplication !== normalizedApplication) {
      const mergedParams = new URLSearchParams(searchParamsKey);
      if (normalizedApplication) {
        mergedParams.set('app', normalizedApplication);
      } else {
        mergedParams.delete('app');
      }
      setSearchParams(mergedParams, { replace: true });
    }
  }, [
    filterAppValue,
    filterValues,
    normalizedApplication,
    normalizedFilterApplication,
    normalizedQueryApplication,
    searchParamsKey,
    setFilters,
    setSearchParams,
  ]);

  useEffect(() => {
    const storage = getBrowserWindow()?.localStorage;
    if (!storage) {
      return;
    }
    if (normalizedApplication) {
      storage.setItem(storageKey, normalizedApplication);
    } else {
      storage.removeItem(storageKey);
    }
  }, [normalizedApplication, storageKey]);

  const handleApplicationChange = useCallback((next: string) => {
    setApplication(normalizeApplicationFilterValue(next));
  }, []);

  const handleStatusChange = useCallback(
    (next: string | null) => {
      if (!setFilters) return;
      const nextFilters: Record<string, unknown> = { ...filterValues };
      if (next) {
        nextFilters.status = next;
      } else {
        delete nextFilters.status;
      }
      setFilters(nextFilters, {}, false);
    },
    [filterValues, setFilters],
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
  if (normalizedApplication) {
    chips.push({
      key: 'app',
      labelPrefix: 'app',
      labelValue: normalizedApplication,
      onRemove: () => setApplication(''),
    });
  }

  const handleClearAll = useCallback(() => {
    setApplication('');
    if (setFilters) {
      setFilters({}, {}, false);
    }
  }, [setFilters]);

  return (
    <Stack spacing={0.5} sx={{ width: '100%' }}>
      <ListToolbar
        left={<StatusTabs value={filterStatusValue} onChange={handleStatusChange} />}
        right={
          <>
            <ApplicationFilter
              storageKey={storageKey}
              records={records}
              value={application}
              onChange={handleApplicationChange}
            />
            <SearchInput value={search} onChange={setSearch} placeholder="Search app, author, image" />
            <RefreshControl onRefresh={handleRefresh} />
          </>
        }
      />
      <ActiveFilterBar chips={chips} onClearAll={chips.length > 0 ? handleClearAll : undefined} />
    </Stack>
  );
};
