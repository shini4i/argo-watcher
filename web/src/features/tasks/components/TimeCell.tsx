import { Box, Typography } from '@mui/material';
import { useTimezone } from '../../../shared/providers/TimezoneProvider';
import { formatRelativeTime } from '../../../shared/utils/time';
import { tokens } from '../../../theme/tokens';
import { EmptyCell } from './EmptyCell';

interface TimeCellProps {
  readonly ts?: number | null;
}

const CLOCK_FORMAT: Intl.DateTimeFormatOptions = {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
};

const FULL_FORMAT: Intl.DateTimeFormatOptions = {
  day: '2-digit',
  month: 'short',
  year: 'numeric',
  ...CLOCK_FORMAT,
};

/**
 * @description The list's "When" column: relative time over the exact clock
 * time, so a row reads at a glance and still pins down the moment. The full
 * date lives in the tooltip, since "3 days ago" alone loses the day.
 */
export const TimeCell = ({ ts }: TimeCellProps) => {
  const { formatDate } = useTimezone();
  if (ts == null) {
    return <EmptyCell />;
  }

  return (
    <Box title={formatDate(ts, FULL_FORMAT)}>
      <Typography variant="body2" sx={{ fontSize: 13, lineHeight: 1.2 }}>
        {formatRelativeTime(ts)}
      </Typography>
      <Typography
        variant="body2"
        sx={{
          fontFamily: tokens.fontMono,
          fontSize: 11,
          color: 'text.secondary',
          lineHeight: 1.3,
          fontVariantNumeric: 'tabular-nums',
        }}
      >
        {formatDate(ts, CLOCK_FORMAT)}
      </Typography>
    </Box>
  );
};
