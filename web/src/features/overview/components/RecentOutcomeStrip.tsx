import { Box, Stack, Tooltip } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { describeTaskStatus } from '../../tasks/utils/statusPresentation';

interface RecentOutcomeStripProps {
  /** Newest first, at most `slots` are drawn. */
  readonly statuses: readonly string[];
  readonly slots?: number;
  readonly size?: number;
}

/**
 * @description The last N outcomes as one square each, oldest on the left so it
 * reads as a timeline. Empty slots stay drawn, so a young application is
 * visibly young rather than looking like a shorter history of successes.
 */
export const RecentOutcomeStrip = ({ statuses, slots = 10, size = 16 }: RecentOutcomeStripProps) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const newestFirst = statuses.slice(0, slots);
  const oldestFirst = [...newestFirst].reverse();
  const missing = Math.max(0, slots - oldestFirst.length);

  return (
    <Stack direction="row" spacing={0.5} aria-label={`Last ${newestFirst.length} outcomes`}>
      {Array.from({ length: missing }, (_unused, index) => (
        <Box
          key={`empty-${index}`}
          aria-hidden
          sx={{
            width: size,
            height: size,
            borderRadius: '3px',
            backgroundColor: isDark ? 'rgba(255,255,255,0.08)' : 'rgba(0,0,0,0.08)',
          }}
        />
      ))}
      {oldestFirst.map((status, index) => {
        const descriptor = describeTaskStatus(status);
        return (
          <Tooltip key={`${status}-${index}`} title={descriptor.displayLabel}>
            <Box
              sx={{
                width: size,
                height: size,
                borderRadius: '3px',
                backgroundColor: isDark ? descriptor.pillFgDark : descriptor.pillFg,
              }}
            />
          </Tooltip>
        );
      })}
    </Stack>
  );
};
