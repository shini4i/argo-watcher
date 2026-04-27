import { Box } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { describeTaskStatus } from '../utils/statusPresentation';
import { tokens } from '../../../theme/tokens';

export interface StatusPillProps {
  readonly status?: string | null;
}

/**
 * Inline status badge that replaces MUI Chip on the task tables. Renders as a
 * 24 px pill so a row of statuses reads as labels (not actionable buttons).
 * Picks light/dark background+foreground from `describeTaskStatus()` based on
 * the active palette mode.
 */
export const StatusPill = ({ status }: StatusPillProps) => {
  const theme = useTheme();
  const presentation = describeTaskStatus(status);
  const isDark = theme.palette.mode === 'dark';
  const bg = isDark ? presentation.pillBgDark : presentation.pillBg;
  const fg = isDark ? presentation.pillFgDark : presentation.pillFg;

  return (
    <Box
      component="span"
      role="status"
      aria-label={presentation.label}
      sx={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 0.75,
        height: 24,
        padding: '4px 10px 4px 8px',
        borderRadius: tokens.radiusPill,
        fontSize: 12,
        fontWeight: 500,
        lineHeight: 1,
        backgroundColor: bg,
        color: fg,
        whiteSpace: 'nowrap',
        '& .MuiSvgIcon-root': {
          fontSize: 14,
        },
        '& .MuiCircularProgress-root': {
          color: 'inherit',
        },
      }}
    >
      <Box component="span" aria-hidden sx={{ display: 'inline-flex', alignItems: 'center' }}>
        {presentation.icon}
      </Box>
      <Box component="span">{presentation.displayLabel}</Box>
    </Box>
  );
};
