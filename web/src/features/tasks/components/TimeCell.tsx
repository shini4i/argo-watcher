import { Stack, Typography } from '@mui/material';
import { useTimezone } from '../../../shared/providers/TimezoneProvider';
import { formatRelativeTime } from '../../../shared/utils/time';
import { tokens } from '../../../theme/tokens';
import { EmptyCell } from './EmptyCell';

export type TimeCellMode = 'both' | 'date' | 'relative';

interface TimeCellProps {
  readonly ts?: number | null;
  readonly relative?: number | null;
  readonly mode?: TimeCellMode;
}

const COMPACT_FORMAT_THIS_YEAR: Intl.DateTimeFormatOptions = {
  day: '2-digit',
  month: 'short',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
};

const COMPACT_FORMAT_OTHER_YEAR: Intl.DateTimeFormatOptions = {
  day: '2-digit',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
};

const FULL_FORMAT: Intl.DateTimeFormatOptions = {
  day: '2-digit',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
};

const isCurrentYear = (ts: number) => {
  const date = new Date(ts < 10_000_000_000 ? ts * 1000 : ts);
  return date.getUTCFullYear() === new Date().getUTCFullYear();
};

/**
 * Time cell with three layouts:
 *   - "both"    → two-line: formatted date + relative-to-now line
 *   - "date"    → single line, full date with year and seconds
 *   - "relative"→ single line, relative-to-now string
 *
 * The mode is selected per-column by the caller (Created vs Updated columns
 * each render only one half of the legacy two-line layout).
 */
export const TimeCell = ({ ts, relative, mode = 'both' }: TimeCellProps) => {
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
        {formatRelativeTime(relative ?? ts)}
      </Typography>
    );
  }

  if (mode === 'date') {
    return (
      <Typography
        variant="body2"
        sx={{ fontSize: 13, lineHeight: 1.2, fontVariantNumeric: 'tabular-nums' }}
      >
        {formatDate(ts, FULL_FORMAT)}
      </Typography>
    );
  }

  const options = isCurrentYear(ts) ? COMPACT_FORMAT_THIS_YEAR : COMPACT_FORMAT_OTHER_YEAR;
  return (
    <Stack spacing={0.25} sx={{ fontVariantNumeric: 'tabular-nums' }}>
      <Typography variant="body2" sx={{ fontSize: 13, lineHeight: 1.2 }}>
        {formatDate(ts, options)}
      </Typography>
      <Typography
        variant="caption"
        sx={{ fontSize: 11.5, color: 'text.secondary', fontFamily: tokens.fontMono }}
      >
        {formatRelativeTime(relative ?? ts)}
      </Typography>
    </Stack>
  );
};
