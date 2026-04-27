import { Box } from '@mui/material';
import { describeTaskStatus } from '../utils/statusPresentation';
import { tokens } from '../../../theme/tokens';

export interface StatusPillProps {
  readonly status?: string | null;
}

/**
 * Inline status badge that replaces MUI Chip on the task tables. Renders as a
 * 24 px pill so a row of statuses reads as labels (not actionable buttons).
 */
export const StatusPill = ({ status }: StatusPillProps) => {
  const presentation = describeTaskStatus(status);

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
        backgroundColor: presentation.pillBg,
        color: presentation.pillFg,
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
