import RestoreIcon from '@mui/icons-material/Restore';
import { Box, Tooltip } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { tokens } from '../../../theme/tokens';

export interface RollbackIndicatorProps {
  readonly isRollback?: boolean;
}

/**
 * @description Chip flagging a deployment that returns to a previously deployed
 * version; renders nothing otherwise. It belongs beside the app name rather than
 * in the status cell, because a rollback qualifies the deployment, not its outcome.
 */
export const RollbackIndicator = ({ isRollback }: RollbackIndicatorProps) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';

  if (!isRollback) {
    return null;
  }

  return (
    <Tooltip title="Rollback to a previously deployed version">
      <Box
        component="span"
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '3px',
          height: 18,
          padding: '0 6px',
          borderRadius: tokens.radiusPill,
          backgroundColor: isDark ? tokens.statusRunningBgDark : tokens.statusRunningBg,
          color: isDark ? tokens.statusRunningFgDark : tokens.statusRunningFg,
          fontSize: 10.5,
          fontWeight: 600,
          whiteSpace: 'nowrap',
          flexShrink: 0,
        }}
      >
        <RestoreIcon aria-hidden sx={{ fontSize: 12 }} />
        Rollback
      </Box>
    </Tooltip>
  );
};
