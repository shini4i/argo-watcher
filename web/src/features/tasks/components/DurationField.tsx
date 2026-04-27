import { useEffect, useState } from 'react';
import { Typography } from '@mui/material';
import type { Task } from '../../../data/types';
import { formatDurationCompact, getBrowserWindow } from '../../../shared/utils';
import { tokens } from '../../../theme/tokens';

interface DurationFieldProps {
  readonly record: Task;
}

/** Returns a 1-second ticker that re-renders the consumer for live duration updates. */
const useNowTicker = (enabled: boolean, intervalMs: number = 1000) => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!enabled) {
      return undefined;
    }
    const browserWindow = getBrowserWindow();
    if (!browserWindow) {
      return undefined;
    }
    const id = browserWindow.setInterval(() => setNow(Date.now()), intervalMs);
    return () => browserWindow.clearInterval(id);
  }, [enabled, intervalMs]);
  return now;
};

/**
 * Live-updating duration in compact monospace form ("1m 04s").
 * In-progress tasks tick every second; completed tasks render statically.
 */
export const DurationField = ({ record }: DurationFieldProps) => {
  const inProgress = record.status === 'in progress' && !record.updated;
  const now = useNowTicker(inProgress);
  const effectiveUpdated = record.updated ?? (inProgress ? Math.floor(now / 1000) : record.created);
  const seconds = Math.max(0, effectiveUpdated - record.created);

  return (
    <Typography
      variant="body2"
      sx={{ fontFamily: tokens.fontMono, fontSize: 11.5, fontVariantNumeric: 'tabular-nums' }}
    >
      {formatDurationCompact(seconds)}
    </Typography>
  );
};
