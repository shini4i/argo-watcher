import { useCallback, useEffect, useState } from 'react';
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

const readStoredInterval = (storageKey: string, fallback: number) => {
  const value = Number.parseInt(getBrowserWindow()?.localStorage?.getItem(storageKey) ?? '', 10);
  return Number.isFinite(value) ? value : fallback;
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

  // Hydrate the interval from localStorage on first mount.
  useEffect(() => {
    const stored = readStoredInterval(storageKey, intervalSec);
    if (stored !== intervalSec) {
      setIntervalSec(stored);
      setRemaining(stored);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Persist interval changes.
  useEffect(() => {
    getBrowserWindow()?.localStorage?.setItem(storageKey, String(intervalSec));
  }, [storageKey, intervalSec]);

  // Reset remaining when interval changes or after a refetch.
  useEffect(() => {
    setRemaining(intervalSec);
  }, [intervalSec, state.lastRefetchedAt]);

  // Tick down once per second when not paused; fire onRefresh when reaching 0.
  useEffect(() => {
    if (intervalSec === 0 || paused) {
      return undefined;
    }
    const browserWindow = getBrowserWindow();
    if (!browserWindow) {
      return undefined;
    }
    const id = browserWindow.setInterval(() => {
      setRemaining(prev => {
        if (prev <= 1) {
          onRefresh();
          markRefetched();
          return intervalSec;
        }
        return prev - 1;
      });
    }, 1000);
    return () => browserWindow.clearInterval(id);
  }, [intervalSec, paused, onRefresh, markRefetched]);

  const handleManualRefresh = useCallback(() => {
    onRefresh();
    markRefetched();
    setRemaining(intervalSec);
  }, [onRefresh, markRefetched, intervalSec]);

  const isOff = intervalSec === 0;
  const success = theme.palette.success.main;
  const muted = theme.palette.text.secondary;
  const dotColor = isOff ? muted : success;
  const labelColor = isOff ? muted : success;
  const label = isOff
    ? 'Paused'
    : paused
      ? `Live · ${remaining}s (paused)`
      : `Live · ${remaining}s`;

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
