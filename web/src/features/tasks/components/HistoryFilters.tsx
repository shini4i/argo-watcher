import { useCallback, useEffect, useMemo, useRef } from 'react';
import { Stack } from '@mui/material';
import { useListContext } from 'react-admin';
import type { Task } from '../../../data/types';
import { useFilterState, type FilterStateSchema } from '../../../shared/hooks/useFilterState';
import { ActiveFilterBar, type FilterChipDescriptor } from './ActiveFilterBar';
import { ApplicationFilter } from './ApplicationFilter';
import { DateRangePicker } from './dateRange/DateRangePicker';
import { ListToolbar } from './ListToolbar';
import { useTaskListContext } from './TaskListContext';
import { useTimezone } from '../../../shared/providers/TimezoneProvider';

interface HistoryFiltersValues extends Record<string, unknown> {
  app: string;
  start: number | null;
  end: number | null;
}

const DEFAULTS: HistoryFiltersValues = { app: '', start: null, end: null };

const SCHEMA: FilterStateSchema<HistoryFiltersValues> = {
  app: {
    fromUrl: raw => raw ?? '',
    toUrl: value => value || null,
    storage: true,
  },
  start: {
    fromUrl: raw => (raw ? Number(raw) : null),
    toUrl: value => (value === null ? null : String(value)),
    urlKey: 'startDate',
    storage: true,
  },
  end: {
    fromUrl: raw => (raw ? Number(raw) : null),
    toUrl: value => (value === null ? null : String(value)),
    urlKey: 'endDate',
    storage: true,
  },
};

const STORAGE_KEY = 'historyTasks';

const TRIGGER_FORMAT: Intl.DateTimeFormatOptions = { day: '2-digit', month: 'short', year: 'numeric' };

/** Date range + application filters powering the history list. */
export const HistoryFilters = () => {
  const { data } = useListContext<Task>();
  const { formatDate } = useTimezone();
  const { registerClearAll } = useTaskListContext();
  const records = useMemo(() => (Array.isArray(data) ? data : []), [data]);

  const { values, applied, apply } = useFilterState({
    storageKey: STORAGE_KEY,
    schema: SCHEMA,
    defaults: DEFAULTS,
  });

  const handleAppChange = useCallback(
    (next: string) => {
      apply({ ...values, app: next });
    },
    [apply, values],
  );

  const handleRangeApply = useCallback(
    (range: { start: number | null; end: number | null }) => {
      apply({ ...values, start: range.start, end: range.end });
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
  if (applied.start !== null && applied.end !== null) {
    chips.push({
      key: 'range',
      labelPrefix: 'range',
      labelValue: `${formatDate(applied.start, TRIGGER_FORMAT)} → ${formatDate(applied.end, TRIGGER_FORMAT)}`,
      onRemove: () => apply({ ...values, start: null, end: null }),
    });
  }

  const handleClearAll = useCallback(() => {
    apply({ app: '', start: null, end: null });
  }, [apply]);

  // `apply` re-identifies on every searchParams/filterValues change; park the
  // handler in a ref and register a stable indirector once so the Datagrid
  // "Clear filters" CTA does not race a transient null ref.
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
        left={
          <DateRangePicker
            value={{ start: applied.start, end: applied.end }}
            onApply={handleRangeApply}
          />
        }
        right={
          <ApplicationFilter
            storageKey={`${STORAGE_KEY}.app`}
            records={records}
            value={applied.app}
            onChange={handleAppChange}
          />
        }
      />
      <ActiveFilterBar chips={chips} onClearAll={chips.length > 0 ? handleClearAll : undefined} />
    </Stack>
  );
};
