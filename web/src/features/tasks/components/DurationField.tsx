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
 *
 * In-progress tasks tick every second and measure against the current time
 * rather than `updated`: the backend stamps `updated` at creation and rewrites it
 * only on a status change, so an in-progress task always has `updated == created`
 * and deriving the duration from it would pin the cell at "0s". Tasks in a final
 * status render statically from the stored `updated` timestamp, and show "—" when
 * the API omits it because the end of such a task is unknowable.
 */
export const DurationField = ({ record }: DurationFieldProps) => {
  const inProgress = record.status === 'in progress';
  const now = useNowTicker(inProgress);
  const endSeconds = inProgress ? Math.floor(now / 1000) : record.updated;
  const seconds = endSeconds ? Math.max(0, endSeconds - record.created) : null;

  return (
    <Typography
      variant="body2"
      sx={{ fontFamily: tokens.fontMono, fontSize: 11.5, fontVariantNumeric: 'tabular-nums' }}
    >
      {seconds === null ? '—' : formatDurationCompact(seconds)}
    </Typography>
  );
};
