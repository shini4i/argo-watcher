import { Typography } from '@mui/material';
import { useTimezone } from '../../../shared/providers/TimezoneProvider';
import { formatRelativeTime } from '../../../shared/utils/time';
import { tokens } from '../../../theme/tokens';
import { EmptyCell } from './EmptyCell';

export type TimeCellMode = 'date' | 'relative';

interface TimeCellProps {
  readonly ts?: number | null;
  readonly mode: TimeCellMode;
}

const FULL_FORMAT: Intl.DateTimeFormatOptions = {
  day: '2-digit',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
};

/**
 * Single-line time cell. The Created column passes `mode="date"` for the
 * absolute timestamp; the Updated column passes `mode="relative"` for the
 * relative-to-now string.
 */
export const TimeCell = ({ ts, mode }: TimeCellProps) => {
  const { formatDate } = useTimezone();
  if (ts == null) {
    return <EmptyCell />;
  }

  if (mode === 'relative') {
    return (
      <Typography
        variant="body2"
        sx={{ fontSize: 12.5, color: 'text.secondary', fontFamily: tokens.fontMono, fontVariantNumeric: 'tabular-nums' }}
      >
        {formatRelativeTime(ts)}
      </Typography>
    );
  }

  return (
    <Typography
      variant="body2"
      sx={{ fontSize: 13, lineHeight: 1.2, fontVariantNumeric: 'tabular-nums' }}
    >
      {formatDate(ts, FULL_FORMAT)}
    </Typography>
  );
};
