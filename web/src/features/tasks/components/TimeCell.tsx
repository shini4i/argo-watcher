import { Stack, Typography } from '@mui/material';
import { useTimezone } from '../../../shared/providers/TimezoneProvider';
import { formatRelativeTime } from '../../../shared/utils/time';
import { tokens } from '../../../theme/tokens';
import { EmptyCell } from './EmptyCell';

interface TimeCellProps {
  readonly ts?: number | null;
  readonly relative?: number | null;
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

const isCurrentYear = (ts: number) => {
  const date = new Date(ts < 10_000_000_000 ? ts * 1000 : ts);
  return date.getUTCFullYear() === new Date().getUTCFullYear();
};

/**
 * Two-line time cell: primary is the formatted date in the user's timezone,
 * secondary is the relative-to-now line (e.g. "2 minutes ago").
 */
export const TimeCell = ({ ts, relative }: TimeCellProps) => {
  const { formatDate } = useTimezone();
  if (ts == null) {
    return <EmptyCell />;
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
