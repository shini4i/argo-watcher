import type { ReactNode } from 'react';
import type { AlertColor } from '@mui/material/Alert';
import type { ChipProps } from '@mui/material/Chip';
import CheckCircleOutlineIcon from '@mui/icons-material/CheckCircleOutline';
import CancelOutlinedIcon from '@mui/icons-material/CancelOutlined';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';
import CircularProgress from '@mui/material/CircularProgress';

export interface TaskStatusPresentation {
  readonly label: string;
  readonly chipColor: ChipProps['color'];
  readonly timelineDotColor: 'default' | 'primary' | 'secondary' | 'error' | 'info' | 'success' | 'warning';
  readonly reasonSeverity: AlertColor;
  readonly icon: ReactNode;
}

/** Fallback rendering instructions when a status is unknown or missing. */
const DEFAULT_PRESENTATION: TaskStatusPresentation = {
  label: 'Unknown',
  chipColor: 'default',
  timelineDotColor: 'default',
  reasonSeverity: 'info',
  icon: <ErrorOutlineIcon fontSize="small" />,
};

/** Describes how a task status should be rendered across chips, timelines, and alerts. */
export const describeTaskStatus = (status?: string | null): TaskStatusPresentation => {
  if (!status) {
    return DEFAULT_PRESENTATION;
  }

  switch (status) {
    case 'deployed':
      return {
        label: 'Deployed',
        chipColor: 'success',
        timelineDotColor: 'success',
        reasonSeverity: 'success',
        icon: <CheckCircleOutlineIcon fontSize="small" />,
      };
    case 'failed':
      return {
        label: 'Failed',
        chipColor: 'error',
        timelineDotColor: 'error',
        reasonSeverity: 'error',
        icon: <CancelOutlinedIcon fontSize="small" />,
      };
    case 'in progress':
      return {
        label: 'In Progress',
        chipColor: 'warning',
        timelineDotColor: 'warning',
        reasonSeverity: 'warning',
        icon: <CircularProgress size={16} color="inherit" />,
      };
    case 'app not found':
      return {
        label: 'App Not Found',
        chipColor: 'default',
        timelineDotColor: 'info',
        reasonSeverity: 'info',
        icon: <ErrorOutlineIcon fontSize="small" />,
      };
    default:
      return {
        label: status,
        chipColor: 'default',
        timelineDotColor: 'default',
        reasonSeverity: 'info',
        icon: <ErrorOutlineIcon fontSize="small" />,
      };
  }
};
