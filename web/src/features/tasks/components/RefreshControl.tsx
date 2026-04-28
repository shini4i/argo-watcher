import { useCallback, useEffect, useRef, useState } from 'react';
import { Box, IconButton, MenuItem, Select, Stack, Typography } from '@mui/material';
import { keyframes, useTheme } from '@mui/material/styles';
import RefreshIcon from '@mui/icons-material/Refresh';
import { tokens } from '../../../theme/tokens';
import { useTaskListContext } from './TaskListContext';
import { getBrowserWindow } from '../../../shared/utils';

const REFRESH_OPTIONS: ReadonlyArray<{ readonly label: string; readonly seconds: number }> = [
  { label: 'Off', seconds: 0 },
  { label: '10s', seconds: 10 },
  { label: '30s', seconds: 30 },
  { label: '1m', seconds: 60 },
  { label: '5m', seconds: 300 },
];

const pulse = keyframes`
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
`;

interface RefreshControlProps {
  readonly onRefresh: () => void;
  readonly storageKey?: string;
}

const ALLOWED_INTERVAL_SECONDS: ReadonlySet<number> = new Set(
  REFRESH_OPTIONS.map(option => option.seconds),
);

// Some browsers (Safari private mode, Firefox with cookies blocked) throw
// SecurityError on any localStorage access. Swallow those failures so the
// countdown still hydrates from the provider default rather than crashing
// the toolbar.
const safeGetItem = (storageKey: string): string | null => {
  try {
    return getBrowserWindow()?.localStorage?.getItem(storageKey) ?? null;
  } catch {
    return null;
  }
};

const safeSetItem = (storageKey: string, value: string): void => {
  try {
    getBrowserWindow()?.localStorage?.setItem(storageKey, value);
  } catch {
    // Ignore — same restricted-storage rationale as safeGetItem.
  }
};

const readStoredInterval = (storageKey: string, fallback: number) => {
  const value = Number.parseInt(safeGetItem(storageKey) ?? '', 10);
  // Only honour values that match a presented option; an unsupported number
  // would render an empty Select and break the countdown.
  return Number.isFinite(value) && ALLOWED_INTERVAL_SECONDS.has(value) ? value : fallback;
};

/**
 * Three-segment refresh pill: live indicator (with pulsing dot + countdown),
 * interval select, manual reload button. Reads pause reasons + interval from
 * the surrounding TaskListProvider so hover/expand can freeze the timer.
 */
export const RefreshControl = ({ onRefresh, storageKey = 'recentTasks.refreshInterval' }: RefreshControlProps) => {
  const theme = useTheme();
  const { state, setInterval: setIntervalSec, markRefetched } = useTaskListContext();
  const { pausedReasons, intervalSec } = state;
  const paused = pausedReasons.size > 0 || intervalSec === 0;

  const [remaining, setRemaining] = useState(intervalSec);

  // Hydrate the interval from localStorage exactly once. A ref guards re-runs
  // so we can keep all closure values in deps (passing lint without disabling)
  // while still committing to the "first mount, current props" semantics —
  // re-hydrating mid-session would overwrite the user's manual selection.
  const hydratedRef = useRef(false);
  useEffect(() => {
    if (hydratedRef.current) return;
    hydratedRef.current = true;
    const stored = readStoredInterval(storageKey, intervalSec);
    if (stored !== intervalSec) {
      setIntervalSec(stored);
      setRemaining(stored);
    }
  }, [storageKey, intervalSec, setIntervalSec]);

  // Persist interval changes.
  useEffect(() => {
    safeSetItem(storageKey, String(intervalSec));
  }, [storageKey, intervalSec]);

  // Reset remaining when interval changes or after a refetch.
  useEffect(() => {
    setRemaining(intervalSec);
  }, [intervalSec, state.lastRefetchedAt]);

  // Tick down once per second when not paused. The state-updater function
  // intentionally has no side effects (firing onRefresh from inside the
  // updater would run twice under StrictMode and is discouraged by React);
  // the separate effect below watches `remaining === 0` to refetch.
  useEffect(() => {
    if (intervalSec === 0 || paused) {
      return undefined;
    }
    const browserWindow = getBrowserWindow();
    if (!browserWindow) {
      return undefined;
    }
    const id = browserWindow.setInterval(() => {
      setRemaining(prev => prev - 1);
    }, 1000);
    return () => browserWindow.clearInterval(id);
  }, [intervalSec, paused]);

  // When the countdown crosses zero, fire the refresh. markRefetched then
  // bumps state.lastRefetchedAt, which the reset effect picks up to seed
  // remaining back to intervalSec — keeping refresh and reset paths uniform.
  useEffect(() => {
    if (remaining > 0 || intervalSec === 0 || paused) {
      return;
    }
    onRefresh();
    markRefetched();
  }, [remaining, intervalSec, paused, onRefresh, markRefetched]);

  const handleManualRefresh = useCallback(() => {
    onRefresh();
    markRefetched();
    setRemaining(intervalSec);
  }, [onRefresh, markRefetched, intervalSec]);

  const isOff = intervalSec === 0;
  const live = theme.palette.mode === 'dark' ? tokens.liveFgDark : tokens.liveFg;
  const muted = theme.palette.text.secondary;
  const dotColor = isOff ? muted : live;
  const labelColor = isOff ? muted : live;
  const liveLabel = paused ? `Live · ${remaining}s (paused)` : `Live · ${remaining}s`;
  const label = isOff ? 'Paused' : liveLabel;

  return (
    <Stack
      direction="row"
      alignItems="center"
      role="group"
      aria-label="Refresh control"
      sx={{
        height: 34,
        borderRadius: `${tokens.radiusMd}px`,
        border: `1px solid ${theme.palette.divider}`,
        backgroundColor: theme.palette.background.paper,
        overflow: 'hidden',
      }}
    >
      <Stack
        direction="row"
        alignItems="center"
        spacing={0.75}
        sx={{ px: 1.25, height: '100%', minWidth: 110 }}
      >
        <Box
          aria-hidden
          sx={{
            width: 8,
            height: 8,
            borderRadius: '50%',
            backgroundColor: dotColor,
            animation: !paused && !isOff ? `${pulse} 1.4s ease-in-out infinite` : 'none',
          }}
        />
        <Typography variant="caption" sx={{ fontSize: 12, color: labelColor, fontVariantNumeric: 'tabular-nums' }}>
          {label}
        </Typography>
      </Stack>
      <Box sx={{ width: '1px', height: 18, backgroundColor: theme.palette.divider }} />
      <Select
        size="small"
        variant="standard"
        disableUnderline
        value={intervalSec}
        onChange={event => setIntervalSec(Number(event.target.value))}
        aria-label="Auto-refresh interval"
        sx={{
          height: '100%',
          px: 1,
          fontSize: 12,
          '& .MuiSelect-select': { paddingY: 0.5, paddingRight: '24px !important' },
        }}
      >
        {REFRESH_OPTIONS.map(option => (
          <MenuItem key={option.label} value={option.seconds}>
            {option.label}
          </MenuItem>
        ))}
      </Select>
      <Box sx={{ width: '1px', height: 18, backgroundColor: theme.palette.divider }} />
      <IconButton
        aria-label="Refresh now"
        size="small"
        onClick={handleManualRefresh}
        sx={{ borderRadius: 0, height: '100%', width: 36 }}
      >
        <RefreshIcon fontSize="small" />
      </IconButton>
    </Stack>
  );
};
