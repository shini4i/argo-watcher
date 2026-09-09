import type { ReactNode } from 'react';
import type { AlertColor } from '@mui/material/Alert';
import type { ChipProps } from '@mui/material/Chip';
import CheckCircleOutlineIcon from '@mui/icons-material/CheckCircleOutlined';
import CancelOutlinedIcon from '@mui/icons-material/CancelOutlined';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutlined';
import CircularProgress from '@mui/material/CircularProgress';
import { tokens } from '../../../theme/tokens';

export interface TaskStatusPresentation {
  readonly label: string;
  readonly displayLabel: string;
  readonly chipColor: ChipProps['color'];
  readonly timelineDotColor: 'default' | 'primary' | 'secondary' | 'error' | 'info' | 'success' | 'warning';
  readonly reasonSeverity: AlertColor;
  readonly icon: ReactNode;
  readonly pillBg: string;
  readonly pillFg: string;
  readonly pillBgDark: string;
  readonly pillFgDark: string;
}

const DEFAULT_PRESENTATION: TaskStatusPresentation = {
  label: 'Unknown',
  displayLabel: 'Unknown',
  chipColor: 'default',
  timelineDotColor: 'default',
  reasonSeverity: 'info',
  icon: <ErrorOutlineIcon fontSize="small" />,
  pillBg: tokens.statusInfoBg,
  pillFg: tokens.statusInfoFg,
  pillBgDark: tokens.statusInfoBgDark,
  pillFgDark: tokens.statusInfoFgDark,
};

/** An unrecognised status is echoed as its own label with neutral styling. */
export const describeTaskStatus = (status?: string | null): TaskStatusPresentation => {
  if (!status) {
    return DEFAULT_PRESENTATION;
  }

  switch (status) {
    case 'deployed':
      return {
        label: 'Deployed',
        displayLabel: 'Deployed',
        chipColor: 'success',
        timelineDotColor: 'success',
        reasonSeverity: 'success',
        icon: <CheckCircleOutlineIcon fontSize="small" />,
        pillBg: tokens.statusDeployedBg,
        pillFg: tokens.statusDeployedFg,
        pillBgDark: tokens.statusDeployedBgDark,
        pillFgDark: tokens.statusDeployedFgDark,
      };
    case 'failed':
      return {
        label: 'Failed',
        displayLabel: 'Failed',
        chipColor: 'error',
        timelineDotColor: 'error',
        reasonSeverity: 'error',
        icon: <CancelOutlinedIcon fontSize="small" />,
        pillBg: tokens.statusFailedBg,
        pillFg: tokens.statusFailedFg,
        pillBgDark: tokens.statusFailedBgDark,
        pillFgDark: tokens.statusFailedFgDark,
      };
    case 'in progress':
      return {
        label: 'In Progress',
        displayLabel: 'Running',
        chipColor: 'warning',
        timelineDotColor: 'warning',
        reasonSeverity: 'warning',
        icon: <CircularProgress size={12} thickness={6} color="inherit" />,
        pillBg: tokens.statusRunningBg,
        pillFg: tokens.statusRunningFg,
        pillBgDark: tokens.statusRunningBgDark,
        pillFgDark: tokens.statusRunningFgDark,
      };
    case 'cancelled':
      return {
        label: 'Cancelled',
        displayLabel: 'Cancelled',
        chipColor: 'default',
        timelineDotColor: 'default',
        reasonSeverity: 'info',
        icon: <CancelOutlinedIcon fontSize="small" />,
        pillBg: tokens.statusInfoBg,
        pillFg: tokens.statusInfoFg,
        pillBgDark: tokens.statusInfoBgDark,
        pillFgDark: tokens.statusInfoFgDark,
      };
    case 'app not found':
      return {
        label: 'App Not Found',
        displayLabel: 'Not found',
        chipColor: 'default',
        timelineDotColor: 'info',
        reasonSeverity: 'info',
        icon: <ErrorOutlineIcon fontSize="small" />,
        pillBg: tokens.statusInfoBg,
        pillFg: tokens.statusInfoFg,
        pillBgDark: tokens.statusInfoBgDark,
        pillFgDark: tokens.statusInfoFgDark,
      };
    default:
      return {
        label: status,
        displayLabel: status,
        chipColor: 'default',
        timelineDotColor: 'default',
        reasonSeverity: 'info',
        icon: <ErrorOutlineIcon fontSize="small" />,
        pillBg: tokens.statusInfoBg,
        pillFg: tokens.statusInfoFg,
        pillBgDark: tokens.statusInfoBgDark,
        pillFgDark: tokens.statusInfoFgDark,
      };
  }
};

/** Statuses whose task never left the watcher successfully. */
const FAILED_STATUSES: ReadonlySet<string> = new Set([
  'failed',
  'aborted',
  'argocd is unavailable',
  'cannot connect to database',
  'failed to login to argocd',
]);

export const isFailedStatus = (status?: string | null): boolean =>
  Boolean(status) && FAILED_STATUSES.has(status!);

export const isRunningStatus = (status?: string | null): boolean => status === 'in progress';

/** Statuses with exactly one cause, which the pill already names in full. */
const SELF_EXPLANATORY_STATUSES: ReadonlySet<string> = new Set(['cancelled']);

/**
 * @description Whether the status pill already conveys the task's single cause,
 * so the list can withhold a reason panel that would only repeat it.
 * @param status the task's status
 * @returns true for a status the pill fully explains on its own
 */
export const statusExplainsItself = (status?: string | null): boolean =>
  Boolean(status) && SELF_EXPLANATORY_STATUSES.has(status!);
