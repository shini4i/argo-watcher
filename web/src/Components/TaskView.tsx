import React, { useContext, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  CircularProgress,
  Container,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Grid,
  Skeleton,
  Stack,
  Tooltip,
  Typography,
} from '@mui/material';
import { AlertColor } from '@mui/material/Alert';
import {
  Timeline,
  TimelineConnector,
  TimelineContent,
  TimelineDot,
  TimelineItem,
  TimelineSeparator,
} from '@mui/lab';
import CheckCircleOutlineIcon from '@mui/icons-material/CheckCircleOutline';
import CancelOutlinedIcon from '@mui/icons-material/CancelOutlined';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';

import { fetchConfig, fetchTask } from '../Services/Data';
import { useErrorContext } from '../ErrorContext';
import { formatDateTime, ProjectDisplay, StatusReasonDisplay } from './TasksTable';
import { AuthContext } from '../Services/Auth';
import { useDeployLock } from '../Services/DeployLockHandler';
import { hasPrivilegedAccess, relativeHumanDuration, relativeTime } from '../Utils';

interface Task {
  id: string;
  created: string;
  updated: string;
  app: string;
  author: string;
  project: string;
  status: string;
  status_reason?: string;
  images: Array<{ image: string; tag: string }>;
}

interface ConfigData {
  argo_cd_url_alias?: string;
  argo_cd_url?: { Scheme: string; Host: string; Path: string };
}

interface ConfigType {
  keycloak: {
    enabled: boolean;
    url: string;
    realm: string;
    client_id: string;
    privileged_groups: string[];
    token_validation_interval: number;
  };
  argo_cd_url_alias?: string;
  argo_cd_url?: { Scheme: string; Host: string; Path: string };
}

/**
 * Type guard ensuring the received value matches the Task structure expected by the view.
 *
 * @param value Arbitrary value returned from the API.
 * @returns True when the value satisfies the Task shape.
 */
const isTask = (value: unknown): value is Task => {
  if (!value || typeof value !== 'object') {
    return false;
  }
  const candidate = value as Record<string, unknown>;
  return ['id', 'created', 'updated', 'app', 'author', 'project', 'status', 'images'].every(
    (key) => key in candidate
  );
};

/**
 * Formats the elapsed time between the provided timestamps into a human readable representation.
 *
 * @param created Unix timestamp in seconds representing when the task started.
 * @param updated Unix timestamp in seconds representing when the task finished or {@code null} when still running.
 * @returns The formatted duration string.
 */
const computeTaskDuration = (created: number, updated: number | null): string => {
  const endTimestamp = updated ?? Math.round(Date.now() / 1000);
  return relativeHumanDuration(endTimestamp - created);
};

type TimelineDotColor = React.ComponentProps<typeof TimelineDot>['color'];

interface StatusDescriptor {
  readonly label: string;
  readonly chipColor: 'default' | 'primary' | 'secondary' | 'error' | 'info' | 'success' | 'warning';
  readonly icon: React.ReactNode;
  readonly timelineDot: TimelineDotColor;
  readonly reasonSeverity: AlertColor;
}

/**
 * Maps API status values onto presentation metadata used throughout the task view.
 *
 * @param status The backend status string.
 * @returns A descriptor covering chip styling, icons and severity cues.
 */
const describeStatus = (status: string): StatusDescriptor => {
  switch (status) {
    case 'deployed':
      return {
        label: 'Deployed',
        chipColor: 'success',
        icon: <CheckCircleOutlineIcon fontSize="small" />,
        timelineDot: 'success',
        reasonSeverity: 'success',
      };
    case 'failed':
      return {
        label: 'Failed',
        chipColor: 'error',
        icon: <CancelOutlinedIcon fontSize="small" />,
        timelineDot: 'error',
        reasonSeverity: 'error',
      };
    case 'in progress':
      return {
        label: 'In Progress',
        chipColor: 'warning',
        icon: <CircularProgress size={16} />,
        timelineDot: 'warning',
        reasonSeverity: 'warning',
      };
    case 'app not found':
      return {
        label: 'App Not Found',
        chipColor: 'info',
        icon: <ErrorOutlineIcon fontSize="small" />,
        timelineDot: 'info',
        reasonSeverity: 'info',
      };
    default:
      return {
        label: status,
        chipColor: 'default',
        icon: <ErrorOutlineIcon fontSize="small" />,
        timelineDot: 'grey',
        reasonSeverity: 'info',
      };
  }
};

interface TimelineEntry {
  readonly key: string;
  readonly label: string;
  readonly timestamp: number;
  readonly dotColor: TimelineDotColor;
}

/**
 * Converts raw timestamp fields provided by the backend into numeric values when possible.
 *
 * @param task The task retrieved from the API.
 * @returns Normalised created and updated timestamps in seconds since epoch.
 */
const computeTaskTimestamps = (task: Task): { created: number | null; updated: number | null } => {
  const createdRaw = Number(task.created);
  const created = Number.isFinite(createdRaw) ? createdRaw : null;

  const updatedRaw = task.updated ? Number(task.updated) : null;
  const updated = updatedRaw !== null && Number.isFinite(updatedRaw) ? updatedRaw : null;

  return { created, updated };
};

/**
 * Calculates the duration label shown in the summary card.
 *
 * @param status Current task status.
 * @param created Created timestamp (seconds).
 * @param updated Updated timestamp (seconds).
 * @returns Human readable duration or the fallback string.
 */
const computeDurationLabelForTask = (
  status: string,
  created: number | null,
  updated: number | null,
): string => {
  if (created === null) {
    return 'Unknown';
  }

  const effectiveUpdated = status === 'in progress' ? null : updated;
  return computeTaskDuration(created, effectiveUpdated);
};

/**
 * Builds the text shown for the last update field.
 *
 * @param updated Updated timestamp (seconds).
 * @returns Formatted label reflecting the last update moment.
 */
const computeLastUpdatedLabel = (updated: number | null): string => {
  if (updated === null) {
    return 'Not yet updated';
  }

  return relativeTime(updated * 1000);
};

/**
 * Generates timeline entries for the summary timeline component.
 *
 * @param created Created timestamp (seconds).
 * @param updated Updated timestamp (seconds).
 * @param descriptor Status descriptor with timeline colour information.
 * @returns Timeline entries in chronological order.
 */
const buildTimelineItems = (
  created: number | null,
  updated: number | null,
  descriptor: StatusDescriptor,
): TimelineEntry[] => {
  const items: TimelineEntry[] = [];

  if (created !== null) {
    items.push({
      key: 'created',
      label: 'Created',
      timestamp: created,
      dotColor: 'info',
    });
  }

  if (updated !== null) {
    items.push({
      key: 'status',
      label: descriptor.label,
      timestamp: updated,
      dotColor: descriptor.timelineDot,
    });
  }

  return items;
};

/**
 * Computes tooltip messaging and disabled state for the rollback button.
 *
 * @param status Current task status.
 * @param deployLock Whether rollout lockdown is active.
 * @returns Tooltip text and disabled flag for the rollback action.
 */
const deriveRollbackState = (status: string, deployLock: boolean): { tooltip: string; disabled: boolean } => {
  if (status === 'in progress') {
    return {
      tooltip: 'Rollback is disabled while the task is running.',
      disabled: true,
    };
  }

  if (deployLock) {
    return {
      tooltip: 'Rollback blocked because lockdown is active.',
      disabled: true,
    };
  }

  return {
    tooltip: '',
    disabled: false,
  };
};

interface InfoFieldProps {
  readonly label: string;
  readonly value: React.ReactNode;
}

/**
 * Renders a compact label/value pair suitable for summary sections.
 *
 * @param props Field label and value.
 * @returns A vertically stacked typography block.
 */
const InfoField: React.FC<InfoFieldProps> = ({ label, value }) => (
  <Box>
    <Typography
      variant="caption"
      color="text.secondary"
      sx={{ textTransform: 'uppercase', letterSpacing: 0.6, fontWeight: 600 }}
    >
      {label}
    </Typography>
    {typeof value === 'string' || typeof value === 'number' ? (
      <Typography variant="body1">{value}</Typography>
    ) : (
      value
    )}
  </Box>
);

/**
 * Displays the detailed task page including summary, metadata, related images and task actions.
 *
 * @returns The rendered task details component.
 */
export default function TaskView() {
  const { id } = useParams<{ id: string }>();
  const [task, setTask] = useState<Task | null>(null);
  const { setError, setSuccess } = useErrorContext();
  const authContext = useContext(AuthContext);

  if (!authContext) {
    throw new Error('AuthContext must be used within an AuthProvider');
  }

  const { authenticated, email, groups, privilegedGroups, keycloakToken } = authContext;
  const [configData, setConfigData] = useState<ConfigData | null>(null);
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);

  const handleClickOpen = () => {
    setOpen(true);
  };

  const handleClose = () => {
    setOpen(false);
  };

  const handleConfirm = async () => {
    setOpen(false);
    await rollbackToVersion();
  };

  const deployLock = useDeployLock();

  useEffect(() => {
    fetchConfig().then((config: ConfigType) => {
      setConfigData(config);
    }).catch(error => {
      setError('fetchConfig', error.message);
    });
  }, [setError]);

  /**
   * Builds the Argo CD URL for the current task based on the configuration response.
   *
   * @returns The fully qualified Argo CD link or an empty string when unavailable.
   */
  const getArgoCDUrl = () => {
    if (configData?.argo_cd_url_alias) {
      return `${configData.argo_cd_url_alias}/applications/${task?.app}`;
    } else if (configData?.argo_cd_url) {
      return `${configData.argo_cd_url.Scheme}://${configData.argo_cd_url.Host}${configData.argo_cd_url.Path}/applications/${task?.app}`;
    }
    return '';
  };

  useEffect(() => {
    if (!id) {
      setError('fetchTask', 'Task identifier is not present in the route.');
      return;
    }

    fetchTask(id)
      .then(item => {
        if (!isTask(item)) {
          throw new Error('Unexpected task payload received from the server.');
        }
        setSuccess('fetchTask', 'Fetched task successfully');
        setTask(item);
      })
      .catch((error: Error) => {
        setError('fetchTasks', error.message);
      });
  }, [id, setError, setSuccess]);

  useEffect(() => {
    let intervalId: NodeJS.Timeout;

    const fetchTaskStatus = async () => {
      if (!id) {
        return;
      }
      console.log('Fetching task status...');
      try {
        const updatedTask = await fetchTask(id);
        if (!isTask(updatedTask)) {
          throw new Error('Unexpected task payload received from the server.');
        }
        setTask(updatedTask);
      } catch (error: any) {
        setError('fetchTaskStatus', error.message);
      }
    };

    if (task?.status === 'in progress' && id) {
      intervalId = setInterval(fetchTaskStatus, 10000);
    }

    return () => {
      if (intervalId) {
        clearInterval(intervalId);
      }
    };
  }, [task, id, setError]);

  /**
   * Requests the backend to redeploy the selected task version under the current user.
   *
   * @throws When the backend rejects the request or returns an unexpected status.
   */
  const rollbackToVersion = async () => {
    if (!task) {
      return;
    }
    if (!email) {
      setError('fetchTasks', 'Unable to rollback without a known author.');
      return;
    }

    const updatedTask = {
      ...task,
      author: email,
    };

    const browserWindow = (globalThis as typeof globalThis & { window?: Window }).window;
    const apiEndpoint =
      browserWindow === undefined
        ? '/api/v1/tasks'
        : `${browserWindow.location.origin}/api/v1/tasks`;

    try {
      const response = await fetch(apiEndpoint, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Keycloak-Authorization': keycloakToken || '',
        },
        body: JSON.stringify(updatedTask),
      });

      if (response.status === 401) {
        throw new Error('You are not authorized to perform this action!');
      } else if (response.status === 406) {
        throw new Error('Lockdown is active. Deployments are forbidden!');
      } else if (response.status !== 202) {
        throw new Error(`Received unexpected status code: ${response.status}`);
      }

      navigate('/');
    } catch (error: any) {
      setError('fetchTasks', error.message);
    }
  };

  if (!task) {
    return (
      <Container maxWidth="lg" sx={{ py: 3 }}>
        <Stack spacing={3}>
          <Card elevation={3}>
            <CardContent>
              <Skeleton variant="text" width="35%" />
              <Skeleton variant="text" width="55%" />
              <Skeleton variant="rectangular" height={80} sx={{ mt: 2 }} />
            </CardContent>
          </Card>
          <Card elevation={3}>
            <CardContent>
              <Skeleton variant="text" width="30%" />
              <Skeleton variant="rectangular" height={120} sx={{ mt: 1 }} />
            </CardContent>
          </Card>
        </Stack>
      </Container>
    );
  }

  const { created: createdTimestamp, updated: updatedTimestamp } = computeTaskTimestamps(task);
  const statusDescriptor = describeStatus(task.status);
  const durationLabel = computeDurationLabelForTask(
    task.status,
    createdTimestamp,
    updatedTimestamp,
  );
  const lastUpdatedLabel = computeLastUpdatedLabel(updatedTimestamp);
  const timelineItems = buildTimelineItems(createdTimestamp, updatedTimestamp, statusDescriptor);
  const argoCdUrl = getArgoCDUrl();
  const rollbackState = deriveRollbackState(task.status, deployLock);
  const rollbackHoverDisabled = rollbackState.tooltip.length === 0;
  const userIsPrivileged = hasPrivilegedAccess(groups, privilegedGroups);

  return (
    <Container maxWidth="lg" sx={{ py: 3 }}>
      <Stack spacing={3}>
        <Card elevation={3}>
          <CardHeader
            title={`Task ${task.id.substring(0, 8)}`}
            subheader="UTC"
            action={
              <Chip
                color={statusDescriptor.chipColor}
                icon={statusDescriptor.icon}
                label={statusDescriptor.label}
                variant="filled"
                sx={{ fontWeight: 600 }}
              />
            }
          />
          <CardContent>
            <Stack spacing={3}>
              <Stack
                direction={{ xs: 'column', md: 'row' }}
                spacing={3}
                justifyContent="space-between"
              >
                <InfoField label="Application" value={task.app} />
                <InfoField label="Author" value={task.author} />
                <InfoField label="Duration" value={durationLabel} />
                <InfoField label="Last Updated" value={lastUpdatedLabel} />
              </Stack>
              {timelineItems.length > 0 ? (
                <Timeline
                  position="right"
                  sx={{
                    px: 0,
                    '& .MuiTimelineItem-root:before': {
                      display: 'none',
                    },
                  }}
                >
                  {timelineItems.map((event, index) => (
                    <TimelineItem key={event.key}>
                      <TimelineSeparator>
                        <TimelineDot color={event.dotColor} />
                        {index < timelineItems.length - 1 && <TimelineConnector />}
                      </TimelineSeparator>
                      <TimelineContent sx={{ py: 0.5 }}>
                        <Typography variant="subtitle2">{event.label}</Typography>
                        <Typography variant="body2" color="text.secondary">
                          {formatDateTime(event.timestamp)}
                        </Typography>
                        <Typography variant="caption" color="text.secondary">
                          {relativeTime(event.timestamp * 1000)}
                        </Typography>
                      </TimelineContent>
                    </TimelineItem>
                  ))}
                </Timeline>
              ) : (
                <Typography variant="body2" color="text.secondary">
                  Timeline information is unavailable for this task.
                </Typography>
              )}
            </Stack>
          </CardContent>
        </Card>

        {task.status_reason && (
          <Alert
            severity={statusDescriptor.reasonSeverity}
            sx={{ alignItems: 'flex-start' }}
          >
            <Typography variant="subtitle2" sx={{ mb: 1 }}>
              Status Details
            </Typography>
            <StatusReasonDisplay reason={task.status_reason} />
          </Alert>
        )}

        <Card elevation={3}>
          <CardHeader title="Task Metadata" />
          <CardContent>
            <Grid container spacing={3}>
              <Grid item xs={12} md={6}>
                <InfoField
                  label="Task ID"
                  value={
                    <Typography variant="body1" sx={{ fontFamily: 'monospace' }}>
                      {task.id}
                    </Typography>
                  }
                />
              </Grid>
              <Grid item xs={12} md={6}>
                <InfoField label="Application" value={task.app} />
              </Grid>
              <Grid item xs={12} md={6}>
                <InfoField label="Author" value={task.author} />
              </Grid>
              <Grid item xs={12} md={6}>
                <InfoField
                  label="Project"
                  value={<ProjectDisplay project={task.project} />}
                />
              </Grid>
              <Grid item xs={12} md={6}>
                <InfoField
                  label="Created (UTC)"
                  value={
                    createdTimestamp ? formatDateTime(createdTimestamp) : 'Not yet created'
                  }
                />
              </Grid>
              <Grid item xs={12} md={6}>
                <InfoField
                  label="Updated (UTC)"
                  value={
                    updatedTimestamp ? formatDateTime(updatedTimestamp) : 'Not yet updated'
                  }
                />
              </Grid>
            </Grid>
          </CardContent>
        </Card>

        <Card elevation={3}>
          <CardHeader title="Images" />
          <CardContent>
            {task.images.length > 0 ? (
              <Stack spacing={1.5}>
                {task.images.map((item) => (
                  <Stack
                    key={`${item.image}:${item.tag}`}
                    direction={{ xs: 'column', sm: 'row' }}
                    spacing={1}
                    alignItems={{ xs: 'flex-start', sm: 'center' }}
                    justifyContent="space-between"
                  >
                    <Typography
                      variant="body2"
                      sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}
                    >
                      {item.image}
                    </Typography>
                    <Chip label={item.tag} size="small" color="primary" variant="outlined" />
                  </Stack>
                ))}
              </Stack>
            ) : (
              <Typography variant="body2" color="text.secondary">
                No container images were reported for this task.
              </Typography>
            )}
          </CardContent>
        </Card>

        <Card elevation={3}>
          <CardHeader title="Actions" />
          <CardContent>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
              {argoCdUrl ? (
                <Button
                  component="a"
                  href={argoCdUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  variant="outlined"
                >
                  Open in Argo CD UI
                </Button>
              ) : (
                <Tooltip title="Argo CD URL is not configured.">
                  <span>
                    <Button variant="outlined" disabled>
                      Open in Argo CD UI
                    </Button>
                  </span>
                </Tooltip>
              )}
              {authenticated && userIsPrivileged && (
                <Tooltip title={rollbackState.tooltip} disableHoverListener={rollbackHoverDisabled}>
                  <span>
                    <Button
                      variant="contained"
                      onClick={handleClickOpen}
                      disabled={rollbackState.disabled}
                    >
                      Rollback to this version
                    </Button>
                  </span>
                </Tooltip>
              )}
            </Stack>
          </CardContent>
        </Card>

        {deployLock && (
          <Alert
            severity="error"
            icon={<ErrorOutlineIcon />}
            sx={{ alignItems: 'center' }}
          >
            Lockdown is active. Deployments are forbidden.
          </Alert>
        )}
      </Stack>

      <Dialog
        open={open}
        onClose={handleClose}
        aria-labelledby="alert-dialog-title"
        aria-describedby="alert-dialog-description"
      >
        <DialogTitle id="alert-dialog-title">{'Rollback Confirmation'}</DialogTitle>
        <DialogContent>
          <DialogContentText id="alert-dialog-description">
            Are you sure you want to rollback to this version?
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleClose}>Cancel</Button>
          <Button onClick={handleConfirm} autoFocus>
            Yes
          </Button>
        </DialogActions>
      </Dialog>
    </Container>
  );
}
