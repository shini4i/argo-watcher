import { Button, Stack, TextField } from '@mui/material';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useListContext } from 'react-admin';
import { useSearchParams } from 'react-router-dom';
import type { Task } from '../../../data/types';
import { ApplicationFilter, readInitialApplication } from './ApplicationFilter';
import { getBrowserWindow } from '../../../shared/utils';

const STORAGE_KEY_APP = 'historyTasks.app';

const toDateValue = (seconds?: number) => {
  if (!seconds) {
    return '';
  }
  return new Date(seconds * 1000).toISOString().slice(0, 10);
};

const startOfDaySeconds = (value: string) => Math.floor(new Date(`${value}T00:00:00Z`).getTime() / 1000);
const endOfDaySeconds = (value: string) => Math.floor(new Date(`${value}T23:59:59Z`).getTime() / 1000);

/**
 * Date range and application filters for the history list, allowing app-only queries when no dates are specified.
 */
/** Date range + application filters powering the history list. */
export const HistoryFilters = () => {
  const { filterValues = {}, setFilters, data } = useListContext<Task>();
  const records = useMemo(() => (Array.isArray(data) ? data : []), [data]);
  const [searchParams, setSearchParams] = useSearchParams();

  const [application, setApplication] = useState(() => searchParams.get('app') ?? readInitialApplication(STORAGE_KEY_APP));
  const [start, setStart] = useState(() => searchParams.get('startDate') ?? toDateValue(filterValues.start as number | undefined));
  const [end, setEnd] = useState(() => searchParams.get('endDate') ?? toDateValue(filterValues.end as number | undefined));

  const syncSearchParams = useCallback(
    (nextStart: string = '', nextEnd: string = '', nextApp: string = '') => {
      const currentStart = searchParams.get('startDate') ?? '';
      const currentEnd = searchParams.get('endDate') ?? '';
      const currentApp = searchParams.get('app') ?? '';

      if (currentStart === nextStart && currentEnd === nextEnd && currentApp === nextApp) {
        return;
      }

      const mergedParams = new URLSearchParams(searchParams);

      if (nextStart) {
        mergedParams.set('startDate', nextStart);
      } else {
        mergedParams.delete('startDate');
      }

      if (nextEnd) {
        mergedParams.set('endDate', nextEnd);
      } else {
        mergedParams.delete('endDate');
      }

      if (nextApp) {
        mergedParams.set('app', nextApp);
      } else {
        mergedParams.delete('app');
      }

      setSearchParams(mergedParams, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const applyFilters = useCallback(() => {
    if (!setFilters) {
      return;
    }

    const nextFilters: Record<string, unknown> = { ...filterValues };

    if (start && end) {
      nextFilters.start = startOfDaySeconds(start);
      nextFilters.end = endOfDaySeconds(end);
    } else {
      delete nextFilters.start;
      delete nextFilters.end;
    }

    const storage = getBrowserWindow()?.localStorage;

    if (application) {
      nextFilters.app = application;
      storage?.setItem(STORAGE_KEY_APP, application);
    } else {
      delete nextFilters.app;
      storage?.removeItem(STORAGE_KEY_APP);
    }

    setFilters(nextFilters, {}, false);
    syncSearchParams(start, end, application);
  }, [application, end, filterValues, setFilters, start, syncSearchParams]);

  useEffect(() => {
    applyFilters();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const normalizedStart = start ?? '';
  const normalizedEnd = end ?? '';
  const normalizedApplication = application ?? '';
  const appliedStart =
    searchParams.get('startDate') ?? toDateValue(filterValues.start as number | undefined);
  const appliedEnd = searchParams.get('endDate') ?? toDateValue(filterValues.end as number | undefined);
  const appliedApplication =
    searchParams.get('app') ?? (typeof filterValues.app === 'string' ? (filterValues.app as string) : '');

  const hasStart = Boolean(normalizedStart);
  const hasEnd = Boolean(normalizedEnd);
  const hasPartialDateRange = (hasStart && !hasEnd) || (!hasStart && hasEnd);
  const hasChanges =
    normalizedStart !== appliedStart ||
    normalizedEnd !== appliedEnd ||
    normalizedApplication !== appliedApplication;
  const isApplyDisabled = hasPartialDateRange || !hasChanges;

  return (
    <Stack
      direction={{ xs: 'column', md: 'row' }}
      spacing={{ xs: 1.5, md: 2 }}
      alignItems={{ xs: 'flex-end', md: 'center' }}
      justifyContent="flex-end"
      sx={{ width: { xs: '100%', md: 'auto' } }}
    >
      <TextField
        label="Start date"
        type="date"
        size="small"
        InputLabelProps={{ shrink: true }}
        value={start}
        onChange={event => setStart(event.target.value)}
      />
      <TextField
        label="End date"
        type="date"
        size="small"
        InputLabelProps={{ shrink: true }}
        value={end}
        onChange={event => setEnd(event.target.value)}
      />
      <ApplicationFilter
        storageKey={STORAGE_KEY_APP}
        records={records}
        value={application}
        onChange={setApplication}
      />
      <Button variant="contained" onClick={applyFilters} disabled={isApplyDisabled}>
        Apply
      </Button>
    </Stack>
  );
};
