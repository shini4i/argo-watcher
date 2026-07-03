import RestoreIcon from '@mui/icons-material/Restore';
import { Box, Tooltip } from '@mui/material';

export interface RollbackIndicatorProps {
  readonly isRollback?: boolean;
}

/**
 * Compact marker rendered next to a task's status to flag that the deployment
 * returns to a previously deployed version. Renders nothing for regular
 * (non-rollback) deployments so it can be dropped into any status cell without
 * reserving layout space.
 */
export const RollbackIndicator = ({ isRollback }: RollbackIndicatorProps) => {
  if (!isRollback) {
    return null;
  }

  return (
    <Tooltip title="Rollback to a previously deployed version">
      <Box
        component="span"
        role="img"
        aria-label="Rollback"
        sx={{ display: 'inline-flex', alignItems: 'center', color: 'warning.main' }}
      >
        <RestoreIcon sx={{ fontSize: 16 }} />
      </Box>
    </Tooltip>
  );
};
