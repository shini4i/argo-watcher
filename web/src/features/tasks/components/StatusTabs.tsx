import { useMemo } from 'react';
import { Stack, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { useGetList, useListContext } from 'react-admin';
import type { Task } from '../../../data/types';
import { tokens } from '../../../theme/tokens';

interface StatusTabsProps {
  /** Current filterValues mirrored from useListContext (so the parent owns reconciliation). */
  readonly value: string | null;
  readonly onChange: (next: string | null) => void;
}

interface TabSpec {
  readonly id: string | null;
  readonly label: string;
  readonly statusFilter?: string;
}

const TABS: ReadonlyArray<TabSpec> = [
  { id: null, label: 'All' },
  { id: 'in progress', label: 'In progress', statusFilter: 'in progress' },
  { id: 'failed', label: 'Failed', statusFilter: 'failed' },
];

// staleTime only dedupes mounts and tab switches: the toolbar's refresh
// invalidates the query, and invalidation refetches regardless of staleTime, so
// this is not a ceiling on how often the wide page below is fetched.
const STATUS_QUERY_OPTS = {
  staleTime: 30_000,
  retry: false,
  refetchOnWindowFocus: false,
} as const;

/** Page size of the count query — the ceiling on client-side status grouping. */
const TASK_COUNT_PAGE_SIZE = 1000;

interface StatusCountSnapshot {
  /** Task count for the parent filters with the status filter ignored. */
  readonly total: number;
  readonly counts: Map<string, number>;
  /** True when the counts are lower bounds because the page cut the result set. */
  readonly truncated: boolean;
  /** True until the first snapshot lands, so pills can avoid claiming zero. */
  readonly unavailable: boolean;
}

/**
 * Strips the status entry from the parent list filter so all counts reflect
 * every status (instead of only the currently selected pill) while still
 * respecting the user's app/time-range filters.
 */
const dropStatusFilter = (filter: Record<string, unknown>): Record<string, unknown> => {
  if (!('status' in filter)) return filter;
  const next = { ...filter };
  delete next.status;
  return next;
};

/**
 * Counts tasks per status from a single `useGetList` call that inherits the
 * parent list's filters minus `status`, so one query feeds every pill.
 *
 * `total` is the backend's own count and stays exact whatever the page size.
 * The per-status counts are grouped from the fetched rows, so a result set wider
 * than `TASK_COUNT_PAGE_SIZE` makes them lower bounds and sets `truncated`.
 */
const useTaskStatusCounts = (): StatusCountSnapshot => {
  const { filterValues } = useListContext<Task>();
  // Re-issue the count query whenever the parent's non-status filters change so
  // every pill stays scoped to the same set of tasks.
  const filter = useMemo(
    () => dropStatusFilter((filterValues ?? {}) as Record<string, unknown>),
    [filterValues],
  );
  const { data, total } = useGetList<Task>(
    'tasks',
    { pagination: { page: 1, perPage: TASK_COUNT_PAGE_SIZE }, filter },
    STATUS_QUERY_OPTS,
  );

  return useMemo(() => {
    const counts = new Map<string, number>();
    (data ?? []).forEach(task => {
      if (!task.status) return;
      counts.set(task.status, (counts.get(task.status) ?? 0) + 1);
    });
    const resolvedTotal = typeof total === 'number' ? total : 0;
    const loaded = Array.isArray(data);
    const truncated = loaded && data.length < resolvedTotal;
    return { total: resolvedTotal, counts, truncated, unavailable: !loaded };
  }, [data, total]);
};

// A pending or failed count query must not render as "0" — that reads as "no
// such tasks" next to a populated grid. `retry` is off, so a failed first fetch
// keeps the placeholder until the next refresh; once a snapshot has landed,
// react-query keeps it and later failures leave the last known counts on screen.
const COUNT_UNAVAILABLE = '—';

const formatCount = (n: number, truncated: boolean) => (truncated ? `${n}+` : String(n));

/**
 * Pill-tab row for filtering the recent list by status. Every count — "All"
 * included — comes from one cached `useGetList` query that ignores the status
 * filter, so selecting a pill never rewrites the other pills' numbers. The
 * parent list context is deliberately not used for "All": its total honours the
 * active status filter and would read 0 on a tab whose status has no tasks.
 */
export const StatusTabs = ({ value, onChange }: StatusTabsProps) => {
  const theme = useTheme();
  const { total, counts: statusCounts, truncated, unavailable } = useTaskStatusCounts();

  const counts: Record<string, number> = {
    all: total,
    'in progress': statusCounts.get('in progress') ?? 0,
    failed: statusCounts.get('failed') ?? 0,
  };

  return (
    <Stack
      direction="row"
      role="tablist"
      aria-label="Status filter"
      spacing={0.5}
      sx={{
        height: 36,
        padding: '3px',
        borderRadius: `${tokens.radiusMd}px`,
        border: `1px solid ${theme.palette.divider}`,
        backgroundColor: theme.palette.mode === 'dark' ? tokens.surface2Dark : tokens.surface2,
      }}
    >
      {TABS.map(tab => {
        const isActive = (value ?? null) === (tab.id ?? null);
        const count = counts[tab.id ?? 'all'] ?? 0;
        // The "All" pill is the query's own total, which is exact regardless of
        // perPage; only the status-grouped pills are counted from the fetched
        // rows and thus subject to truncation, so only they get the "+".
        const showTruncation = tab.id !== null && truncated;
        const isDark = theme.palette.mode === 'dark';
        const activeBg = isDark ? theme.palette.background.paper : tokens.surface;
        const tabBg = isActive ? activeBg : 'transparent';
        const activeCountBg = isDark ? tokens.accentSoftDark : tokens.accentSoft;
        const idleCountBg = isDark ? 'rgba(255, 255, 255, 0.08)' : 'rgba(0, 0, 0, 0.04)';
        const countBg = isActive ? activeCountBg : idleCountBg;
        return (
          <button
            type="button"
            role="tab"
            aria-selected={isActive}
            key={tab.label}
            onClick={() => onChange(tab.id)}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              border: 'none',
              padding: '4px 12px',
              borderRadius: tokens.radiusSm,
              fontSize: 12.5,
              fontFamily: tokens.fontSans,
              fontWeight: isActive ? 600 : 500,
              cursor: 'pointer',
              backgroundColor: tabBg,
              color: isActive ? theme.palette.text.primary : theme.palette.text.secondary,
              boxShadow: isActive ? '0 1px 2px rgba(15, 23, 42, 0.08)' : 'none',
              transition: 'background-color 150ms ease, color 150ms ease',
            }}
          >
            {/* Whitespace-only text nodes generate no flex item, so this
                separates label from count in the accessible name only. */}
            {tab.label}{' '}
            <Typography
              component="span"
              // The placeholder glyph is announced as "em dash" at best, so give
              // screen readers the reason for the missing number instead.
              aria-label={unavailable ? 'count unavailable' : undefined}
              sx={{
                fontFamily: tokens.fontMono,
                fontSize: 11,
                lineHeight: 1,
                padding: '1px 6px',
                borderRadius: tokens.radiusPill,
                backgroundColor: countBg,
                color: isActive ? tokens.accent : 'inherit',
              }}
            >
              {unavailable ? COUNT_UNAVAILABLE : formatCount(count, showTruncation)}
            </Typography>
          </button>
        );
      })}
    </Stack>
  );
};
