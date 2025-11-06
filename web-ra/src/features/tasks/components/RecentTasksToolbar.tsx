import { useCallback, useEffect, useMemo, useState } from 'react';
import { IconButton, MenuItem, Select, Stack } from '@mui/material';
import RefreshIcon from '@mui/icons-material/Refresh';
import { useListContext } from 'react-admin';
import { useSearchParams } from 'react-router-dom';
import { ApplicationFilter, readInitialApplication } from './ApplicationFilter';
import type { Task } from '../../../data/types';
import { useAutoRefresh } from '../../../shared/hooks/useAutoRefresh';

const STORAGE_KEY_INTERVAL = 'recentTasks.refreshInterval';
const DEFAULT_REFRESH = 30;

const AUTO_REFRESH_OPTIONS: Array<{ label: string; seconds: number }> = [
  { label: 'Off', seconds: 0 },
  { label: '10s', seconds: 10 },
  { label: '30s', seconds: 30 },
  { label: '1m', seconds: 60 },
  { label: '5m', seconds: 300 },
];

const readStoredRefreshInterval = () => {
  if (typeof window === 'undefined') {
    return DEFAULT_REFRESH;
  }
  const value = Number.parseInt(window.localStorage.getItem(STORAGE_KEY_INTERVAL) ?? '', 10);
  return Number.isFinite(value) ? value : DEFAULT_REFRESH;
};

/**
 * Action toolbar for the recent tasks list providing application filtering, manual refresh, and auto-refresh controls.
 */
export const RecentTasksToolbar = ({ storageKey = 'recentTasks.app' }: { storageKey?: string }) => {
  const { data, filterValues, setFilters, refetch } = useListContext<Task>();
  const records = useMemo(() => (Array.isArray(data) ? data : []), [data]);

  const [searchParams, setSearchParams] = useSearchParams();
  const [application, setApplication] = useState<string>(() => searchParams.get('app') ?? filterValues?.app ?? readInitialApplication(storageKey));
  const [refreshInterval, setRefreshInterval] = useState<number>(readStoredRefreshInterval);

  useEffect(() => {
    const next = application || undefined;
    if (filterValues?.app !== next) {
      setFilters?.({ ...filterValues, app: next }, {}, false);
    }

    const currentAppParam = searchParams.get('app') ?? undefined;
    if (currentAppParam !== next) {
      const mergedParams = new URLSearchParams(searchParams);
      if (next) {
        mergedParams.set('app', next);
      } else {
        mergedParams.delete('app');
      }
      setSearchParams(mergedParams, { replace: true });
    }
  }, [application, filterValues, searchParams, setFilters, setSearchParams]);

  useEffect(() => {
    window.localStorage.setItem(STORAGE_KEY_INTERVAL, String(refreshInterval));
  }, [refreshInterval]);

  useAutoRefresh(
    refreshInterval,
    useCallback(() => {
      void refetch?.();
    }, [refetch]),
  );

  const handleApplicationChange = useCallback((next: string) => {
    setApplication(next);
  }, []);

  const handleManualRefresh = useCallback(() => {
    void refetch?.();
  }, [refetch]);

  return (
    <Stack
      direction={{ xs: 'column', md: 'row' }}
      spacing={{ xs: 1.5, md: 2 }}
      alignItems={{ xs: 'flex-end', md: 'center' }}
      justifyContent="flex-end"
      sx={{ width: { xs: '100%', md: 'auto' } }}
    >
      <ApplicationFilter
        storageKey={storageKey}
        records={records}
        value={application}
        onChange={handleApplicationChange}
      />
      <Stack direction="row" spacing={1} alignItems="center">
        <Select
          size="small"
          value={refreshInterval}
          onChange={event => setRefreshInterval(Number(event.target.value))}
        >
          {AUTO_REFRESH_OPTIONS.map(option => (
            <MenuItem key={option.label} value={option.seconds}>
              {option.label}
            </MenuItem>
          ))}
        </Select>
        <IconButton aria-label="Refresh now" size="small" color="primary" onClick={handleManualRefresh}>
          <RefreshIcon fontSize="small" />
        </IconButton>
      </Stack>
    </Stack>
  );
};
