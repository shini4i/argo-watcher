import RestoreIcon from '@mui/icons-material/Restore';
import { Box, Tooltip } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { tokens } from '../../../theme/tokens';

export interface RollbackIndicatorProps {
  readonly isRollback?: boolean;
  /** `medium` matches StatusPill, for placing the badge beside one. */
  readonly size?: 'small' | 'medium';
}

/** `medium` mirrors StatusPill's metrics so the two read as one row of labels. */
const SIZES = {
  small: { height: 18, padding: '0 6px', fontSize: 10.5, iconSize: 12, gap: '3px' },
  medium: { height: 24, padding: '0 10px', fontSize: 12, iconSize: 14, gap: '4px' },
} as const;

/**
 * @description Chip flagging a deployment that returns to a previously deployed
 * version; renders nothing otherwise. It qualifies the deployment rather than its
 * outcome, so it accompanies the application name rather than the status.
 * @param size `medium` where it sits beside a StatusPill, `small` in a table row
 */
export const RollbackIndicator = ({ isRollback, size = 'small' }: RollbackIndicatorProps) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';

  if (!isRollback) {
    return null;
  }

  const metrics = SIZES[size];

  return (
    <Tooltip title="Rollback to a previously deployed version">
      <Box
        component="span"
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: metrics.gap,
          height: metrics.height,
          padding: metrics.padding,
          borderRadius: tokens.radiusPill,
          backgroundColor: isDark ? tokens.statusRunningBgDark : tokens.statusRunningBg,
          color: isDark ? tokens.statusRunningFgDark : tokens.statusRunningFg,
          fontSize: metrics.fontSize,
          fontWeight: 600,
          whiteSpace: 'nowrap',
          flexShrink: 0,
        }}
      >
        <RestoreIcon aria-hidden sx={{ fontSize: metrics.iconSize }} />
        Rollback
      </Box>
    </Tooltip>
  );
};
