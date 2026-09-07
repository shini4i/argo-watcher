import { Box, Button, CircularProgress, Stack, Tooltip, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import LaunchIcon from '@mui/icons-material/Launch';
import RestoreIcon from '@mui/icons-material/Restore';
import type { TaskStatus } from '../../../../data/types';
import { tokens } from '../../../../theme/tokens';
import { deriveMonogram } from '../../components/AppCell';
import { StatusPill } from '../../components/StatusPill';
import { RollbackIndicator } from '../../components/RollbackIndicator';
import { CopyChip } from './CopyChip';

interface TaskHeaderProps {
  readonly task: TaskStatus;
  readonly subLine: string;
  readonly argoCdUrl: string | null;
  readonly showRedeploy: boolean;
  readonly redeployDisabled: boolean;
  readonly redeployTooltip: string;
  readonly redeployLoading: boolean;
  readonly onRedeploy: () => void;
}

const hashIndex = (name: string, modulo: number): number => {
  let hash = 0;
  for (let i = 0; i < name.length; i += 1) {
    hash = Math.imul(hash, 31) + (name.codePointAt(i) ?? 0);
  }
  return Math.abs(hash) % modulo;
};

/**
 * @description Identity and actions for the task: the app name is the title,
 * not the truncated task id, and the id becomes a copy chip for support. There
 * is exactly one re-deploy action — a "retry" would issue the same request.
 */
export const TaskHeader = ({
  task,
  subLine,
  argoCdUrl,
  showRedeploy,
  redeployDisabled,
  redeployTooltip,
  redeployLoading,
  onRedeploy,
}: TaskHeaderProps) => {
  const theme = useTheme();
  const app = task.app ?? 'Unknown';
  const swatches =
    theme.palette.mode === 'dark' ? tokens.monogramSwatchesDark : tokens.monogramSwatches;
  const swatch = swatches[hashIndex(app, swatches.length)];

  return (
    <Stack
      direction={{ xs: 'column', md: 'row' }}
      spacing={2}
      sx={{ alignItems: { xs: 'flex-start', md: 'center' } }}
    >
      <Box
        aria-hidden
        sx={{
          width: 32,
          height: 32,
          flexShrink: 0,
          borderRadius: `${tokens.radiusSm}px`,
          backgroundColor: swatch.bg,
          color: swatch.fg,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: 13,
          fontWeight: 600,
        }}
      >
        {deriveMonogram(app)}
      </Box>

      <Box sx={{ minWidth: 0, flexGrow: 1 }}>
        <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
          <Typography component="h1" sx={{ fontSize: 24, fontWeight: 600, minWidth: 0 }}>
            {app}
          </Typography>
          <StatusPill status={task.status} />
          <RollbackIndicator isRollback={task.is_rollback} />
        </Stack>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap', mt: 0.5 }}>
          {task.id && <CopyChip value={task.id} label="Task id" />}
          <Typography sx={{ fontSize: 12.5, color: 'text.secondary' }}>{subLine}</Typography>
        </Stack>
      </Box>

      <Stack direction="row" spacing={1} sx={{ flexShrink: 0 }}>
        {argoCdUrl ? (
          <Button
            component="a"
            href={argoCdUrl}
            target="_blank"
            rel="noopener noreferrer"
            variant="outlined"
            startIcon={<LaunchIcon fontSize="small" />}
          >
            Argo CD
          </Button>
        ) : (
          <Tooltip title="Argo CD URL is not configured for this environment.">
            <span>
              <Button variant="outlined" disabled startIcon={<LaunchIcon fontSize="small" />}>
                Argo CD
              </Button>
            </span>
          </Tooltip>
        )}
        {showRedeploy && (
          <Tooltip title={redeployTooltip} disableHoverListener={!redeployTooltip}>
            <span>
              <Button
                variant="contained"
                onClick={onRedeploy}
                disabled={redeployDisabled}
                startIcon={
                  redeployLoading ? <CircularProgress size={16} /> : <RestoreIcon fontSize="small" />
                }
              >
                Deploy this version again
              </Button>
            </span>
          </Tooltip>
        )}
      </Stack>
    </Stack>
  );
};
