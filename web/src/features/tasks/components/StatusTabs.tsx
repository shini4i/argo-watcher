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

const STATUS_QUERY_OPTS = {
  staleTime: 30_000,
  retry: false,
  refetchOnWindowFocus: false,
} as const;

/**
 * Single `useGetList` call (no status filter) with a wide page so we can
 * group statuses client-side. Cached for 30 s like the previous per-tab
 * queries. The data reuses the same query key as long as no status filter
 * is set, so callers that already paged through `tasks` share the cache.
 */
const useTaskStatusCounts = (): Map<string, number> => {
  const { data } = useGetList<Task>(
    'tasks',
    { pagination: { page: 1, perPage: 1000 } },
    STATUS_QUERY_OPTS,
  );

  return useMemo(() => {
    const counts = new Map<string, number>();
    (data ?? []).forEach(task => {
      if (!task.status) return;
      counts.set(task.status, (counts.get(task.status) ?? 0) + 1);
    });
    return counts;
  }, [data]);
};

/**
 * Pill-tab row for filtering the recent list by status. The "All" total comes
 * from the parent list context; per-status counts come from one cached
 * `useGetList` query that we group by status in memory.
 */
export const StatusTabs = ({ value, onChange }: StatusTabsProps) => {
  const theme = useTheme();
  const { total: listTotal = 0 } = useListContext<Task>();
  const statusCounts = useTaskStatusCounts();

  const counts: Record<string, number> = {
    all: listTotal,
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
              backgroundColor: isActive
                ? theme.palette.mode === 'dark'
                  ? theme.palette.background.paper
                  : tokens.surface
                : 'transparent',
              color: isActive ? theme.palette.text.primary : theme.palette.text.secondary,
              boxShadow: isActive ? '0 1px 2px rgba(15, 23, 42, 0.08)' : 'none',
              transition: 'background-color 150ms ease, color 150ms ease',
            }}
          >
            {tab.label}
            <Typography
              component="span"
              sx={{
                fontFamily: tokens.fontMono,
                fontSize: 11,
                lineHeight: 1,
                padding: '1px 6px',
                borderRadius: tokens.radiusPill,
                backgroundColor: isActive
                  ? theme.palette.mode === 'dark'
                    ? tokens.accentSoftDark
                    : tokens.accentSoft
                  : theme.palette.mode === 'dark'
                    ? 'rgba(255, 255, 255, 0.08)'
                    : 'rgba(0, 0, 0, 0.04)',
                color: isActive ? tokens.accent : 'inherit',
              }}
            >
              {count}
            </Typography>
          </button>
        );
      })}
    </Stack>
  );
};
