import { Box, Stack, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { tokens } from '../../../../theme/tokens';
import { formatDuration, formatRelativeTime } from '../../../../shared/utils/time';

interface TaskLifecycleProps {
  readonly createdLabel: string;
  readonly created: number;
  readonly terminalLabel: string;
  readonly terminalLabelColor: string;
  readonly terminalTime?: string;
  readonly terminalTimestamp?: number | null;
  readonly durationSeconds: number | null;
  readonly isRunning: boolean;
}

const Dot = ({ color }: { color: string }) => (
  <Box
    aria-hidden
    sx={{ width: 10, height: 10, borderRadius: '50%', backgroundColor: color, flexShrink: 0 }}
  />
);

/**
 * @description Horizontal two-stop timeline: task creation, the span the
 * watcher spent polling Argo CD, and the terminal state. Same two timestamps
 * the vertical timeline showed — there are no intermediate events to draw.
 */
export const TaskLifecycle = ({
  createdLabel,
  created,
  terminalLabel,
  terminalLabelColor,
  terminalTime,
  terminalTimestamp,
  durationSeconds,
  isRunning,
}: TaskLifecycleProps) => {
  const theme = useTheme();
  const infoColor = theme.palette.info.main;

  return (
    <Stack
      direction={{ xs: 'column', sm: 'row' }}
      spacing={1.5}
      sx={{
        alignItems: { xs: 'flex-start', sm: 'center' },
        border: `1px solid ${theme.palette.divider}`,
        borderRadius: `${tokens.radiusMd}px`,
        padding: 2,
      }}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexShrink: 0 }}>
        <Dot color={infoColor} />
        <Box>
          <Typography sx={{ fontSize: 13, fontWeight: 600 }}>Task created</Typography>
          <Typography sx={{ fontFamily: tokens.fontMono, fontSize: 11, color: 'text.secondary' }}>
            {createdLabel} · {formatRelativeTime(created)}
          </Typography>
        </Box>
      </Stack>

      <Box sx={{ flexGrow: 1, minWidth: 80, width: { xs: '100%', sm: 'auto' } }}>
        <Typography
          sx={{ fontSize: 11, color: 'text.secondary', textAlign: 'center', mb: 0.5 }}
        >
          watching Argo CD{durationSeconds !== null ? ` · ${formatDuration(durationSeconds)}` : ''}
        </Typography>
        <Box
          aria-hidden
          sx={{
            height: 2,
            borderRadius: 1,
            ...(isRunning
              ? {
                  backgroundImage: `repeating-linear-gradient(90deg, ${terminalLabelColor} 0 6px, transparent 6px 12px)`,
                }
              : {
                  backgroundImage: `linear-gradient(90deg, ${infoColor}, ${terminalLabelColor})`,
                }),
          }}
        />
      </Box>

      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexShrink: 0 }}>
        <Dot color={terminalLabelColor} />
        <Box>
          <Typography sx={{ fontSize: 13, fontWeight: 600, color: terminalLabelColor }}>
            {terminalLabel}
          </Typography>
          <Typography sx={{ fontFamily: tokens.fontMono, fontSize: 11, color: 'text.secondary' }}>
            {isRunning || !terminalTime
              ? 'in progress'
              : `${terminalTime}${terminalTimestamp ? ` · ${formatRelativeTime(terminalTimestamp)}` : ''}`}
          </Typography>
        </Box>
      </Stack>
    </Stack>
  );
};
