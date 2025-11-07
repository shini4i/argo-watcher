import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import FiberManualRecordIcon from '@mui/icons-material/FiberManualRecord';
import RefreshIcon from '@mui/icons-material/Refresh';
import LaunchIcon from '@mui/icons-material/Launch';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Divider,
  Grid,
  Link,
  Stack,
  Tooltip,
  Typography,
} from '@mui/material';
import { useCallback, useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { useGetIdentity, useGetOne, useNotify, usePermissions } from 'react-admin';
import { useNavigate, useParams } from 'react-router-dom';
import type { TaskStatus } from '../../../data/types';
import { formatDuration, formatRelativeTime } from '../../../shared/utils/time';
import { describeTaskStatus } from '../utils/statusPresentation';
import { useDeployLockState } from '../../deployLock/useDeployLockState';
import { useKeycloakEnabled } from '../../../shared/hooks/useKeycloakEnabled';
import { hasPrivilegedAccess } from '../../../shared/utils/permissions';
import { httpClient } from '../../../data/httpClient';
import { getAccessToken } from '../../../auth/tokenStore';
import { useTimezone } from '../../../shared/providers/TimezoneProvider';

interface TimelineEntry {
  readonly id: string;
  readonly label: string;
  readonly timestamp: number;
  readonly color: 'default' | 'primary' | 'secondary' | 'error' | 'info' | 'success' | 'warning';
}

/** Casts different timestamp representations to seconds, returning null when invalid. */
const normalizeTimestamp = (value: unknown): number | null => {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value;
  }

  if (typeof value === 'string') {
    const parsed = Number.parseFloat(value);
    if (Number.isFinite(parsed)) {
      return parsed;
    }
  }

  if (value instanceof Date) {
    return Math.floor(value.getTime() / 1000);
  }

  return null;
};

const MAX_IMAGES_RENDERED = 20;

interface RollbackState {
  disabled: boolean;
  message: string;
}

interface ConfigResponse {
  argo_cd_url_alias?: string;
  argo_cd_url?: {
    Scheme?: string;
    Host?: string;
    Path?: string;
  };
}

const computeRollbackState = (
  status: string | null,
  deployLock: boolean,
  hasEmail: boolean,
): RollbackState => {
  if (status === 'in progress') {
    return {
      disabled: true,
      message: 'Rollback is disabled while the task is running.',
    };
  }

  if (deployLock) {
    return {
      disabled: true,
      message: 'Rollback blocked because lockdown is active.',
    };
  }

  if (!hasEmail) {
    return {
      disabled: true,
      message: 'Unable to rollback without a known author.',
    };
  }

  return {
    disabled: false,
    message: '',
  };
};

/** Builds the Argo CD application deep link from the config payload when possible. */
const buildArgoCdUrl = (config: ConfigResponse | null, app?: string | null): string | null => {
  if (!config || !app) {
    return null;
  }

  if (typeof config.argo_cd_url_alias === 'string' && config.argo_cd_url_alias.length > 0) {
    return `${config.argo_cd_url_alias.replace(/\/$/, '')}/applications/${app}`;
  }

  const { Scheme, Host, Path } = config.argo_cd_url ?? {};
  if (Scheme && Host) {
    const normalizedPath = Path ?? '';
    return `${Scheme}://${Host}${normalizedPath}/applications/${app}`;
  }

  return null;
};

/** Derives the elapsed duration for the task, accounting for in-progress polling. */
const computeDurationSeconds = (
  status: string | null,
  created: number | null,
  updated: number | null,
): number | null => {
  if (created === null) {
    return null;
  }

  const nowSeconds = Math.floor(Date.now() / 1000);
  const effectiveUpdated = status === 'in progress' || updated === null ? nowSeconds : updated;
  return Math.max(0, effectiveUpdated - created);
};

/** Produces timeline entries for the created and updated timestamps shown in the UI. */
const buildTimelineEntries = (
  created: number | null,
  updated: number | null,
  descriptor: ReturnType<typeof describeTaskStatus>,
): TimelineEntry[] => {
  const entries: TimelineEntry[] = [];
  if (created !== null) {
    entries.push({
      id: 'created',
      label: 'Created',
      timestamp: created,
      color: 'info',
    });
  }

  if (updated !== null) {
    entries.push({
      id: 'status',
      label: descriptor.label,
      timestamp: updated,
      color: descriptor.timelineDotColor,
    });
  }

  return entries;
};

/** Displays the task detail screen at `/task/:id` mirroring the legacy Task View experience. */
export const TaskShow = () => {
  const { id } = useParams<{ id: string }>();
  const notify = useNotify();
  const navigate = useNavigate();
  const deployLock = useDeployLockState();
  const keycloakEnabled = useKeycloakEnabled();
  const { permissions } = usePermissions();
  const { data: identity } = useGetIdentity();
  const { formatDate } = useTimezone();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [rollbackLoading, setRollbackLoading] = useState(false);
  const [configData, setConfigData] = useState<ConfigResponse | null>(null);

  const {
    data,
    isLoading,
    isError,
    error,
    refetch,
  } = useGetOne<TaskStatus>('tasks', { id: id ?? '' }, { retry: false, enabled: Boolean(id) });

  useEffect(() => {
    if (isError && error) {
      notify('Failed to load task details.', { type: 'error' });
    }
  }, [error, isError, notify]);

  useEffect(() => {
    let cancelled = false;

    httpClient<ConfigResponse>('/api/v1/config')
      .then(response => {
        if (!cancelled) {
          setConfigData(response.data ?? null);
        }
      })
      .catch(err => {
        if (cancelled) {
          return;
        }
        const message = err instanceof Error ? err.message : 'Failed to load configuration.';
        notify(message, { type: 'warning' });
      });

    return () => {
      cancelled = true;
    };
  }, [notify]);

  const status = data?.status ?? null;
  const identityEmail = identity?.email ?? '';
  const groups: readonly string[] = (permissions as { groups?: string[] })?.groups ?? [];
  const privilegedGroups: readonly string[] =
    (permissions as { privilegedGroups?: string[] })?.privilegedGroups ?? [];
  const userIsPrivileged = hasPrivilegedAccess(groups, privilegedGroups);
  const showRollbackButton = keycloakEnabled && userIsPrivileged;
  const descriptor = describeTaskStatus(status);
  const createdTimestamp = normalizeTimestamp(data?.created);
  const updatedTimestamp = normalizeTimestamp(data?.updated);
  const durationSeconds = computeDurationSeconds(status, createdTimestamp, updatedTimestamp);
  const timelineEntries = buildTimelineEntries(createdTimestamp, updatedTimestamp, descriptor);
  const displayedImages = (data?.images ?? []).slice(0, MAX_IMAGES_RENDERED);
  const hasAdditionalImages = (data?.images?.length ?? 0) > displayedImages.length;
  const argoCdUrl = buildArgoCdUrl(configData, data?.app);
  const rollbackState = computeRollbackState(status, deployLock, Boolean(identityEmail));
  const rollbackDisabled = rollbackState.disabled || rollbackLoading;
  const rollbackTooltip = rollbackDisabled && !rollbackLoading ? rollbackState.message : '';

  useEffect(() => {
    if (!id || status !== 'in progress') {
      return;
    }

    const intervalId = window.setInterval(() => {
      void refetch();
    }, 10_000);

    return () => window.clearInterval(intervalId);
  }, [id, refetch, status]);

  const handleBack = useCallback(() => {
    navigate(-1);
  }, [navigate]);

  const handleRefresh = useCallback(() => {
    void refetch();
  }, [refetch]);

  const handleOpenConfirm = useCallback(() => {
    setConfirmOpen(true);
  }, []);

  const handleCloseConfirm = useCallback(() => {
    if (!rollbackLoading) {
      setConfirmOpen(false);
    }
  }, [rollbackLoading]);

  const handleConfirmRollback = useCallback(async () => {
    if (!data) {
      return;
    }

    if (!identityEmail) {
      notify('Unable to rollback without a known author.', { type: 'warning' });
      setConfirmOpen(false);
      return;
    }

    try {
      setRollbackLoading(true);
      const headers: Record<string, string> = {};
      const token = getAccessToken();
      if (keycloakEnabled && token) {
        headers['Keycloak-Authorization'] = `Bearer ${token}`;
      }

      await httpClient('/api/v1/tasks', {
        method: 'POST',
        headers: Object.keys(headers).length > 0 ? headers : undefined,
        body: {
          ...data,
          author: identityEmail,
        },
      });

      notify('Rollback requested. Monitor the task list for progress updates.', { type: 'info' });
      setConfirmOpen(false);
      navigate('/');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to request rollback.';
      notify(message, { type: 'error' });
    } finally {
      setRollbackLoading(false);
    }
  }, [data, identityEmail, keycloakEnabled, navigate, notify]);

  if (!id) {
    return (
      <Card>
        <CardContent>
          <Typography variant="h6">Task not specified</Typography>
          <Typography variant="body2" color="text.secondary">
            The requested task cannot be located because the identifier is missing from the URL.
          </Typography>
        </CardContent>
      </Card>
    );
  }

  if (isLoading) {
    return (
      <Stack spacing={2} alignItems="center" sx={{ py: 6 }}>
        <CircularProgress />
        <Typography variant="body1">Loading task details…</Typography>
      </Stack>
    );
  }

  if (!data) {
    return (
      <Card>
        <CardContent>
          <Typography variant="h6">Task not found</Typography>
          <Typography variant="body2" color="text.secondary">
            The task with identifier <strong>{id}</strong> could not be located.
          </Typography>
        </CardContent>
      </Card>
    );
  }

  return (
    <Stack spacing={3} sx={{ mt: { xs: 1.5, sm: 2 }, px: { xs: 1, md: 0 } }}>
      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5} alignItems="center">
        <Stack direction="row" spacing={1} flexWrap="wrap">
          <Button onClick={handleBack} startIcon={<ArrowBackIcon />} variant="text">
            Back
          </Button>
          <Button onClick={handleRefresh} startIcon={<RefreshIcon fontSize="small" />} variant="outlined">
            Refresh
          </Button>
        </Stack>
      </Stack>

      <Card elevation={3}>
        <CardHeader
          title={`Task ${data.id?.slice(0, 8) ?? '—'}`}
          subheader="UTC"
          action={<Chip label={descriptor.label} color={descriptor.chipColor} size="medium" icon={descriptor.icon as ReactNode} />}
        />
        <CardContent>
          <Stack spacing={3}>
            <Grid container spacing={3}>
              <Grid item xs={12} md={6}>
                <Stack spacing={1.5}>
                  <InfoField label="Application" value={data.app ?? 'Unknown'} />
                  <InfoField label="Project" value={<ProjectReference project={data.project} />} />
                  <InfoField label="Author" value={data.author ?? '—'} />
                </Stack>
              </Grid>
              <Grid item xs={12} md={6}>
                <Stack spacing={1.5}>
                  <InfoField label="Task ID" value={data.id} />
                  <InfoField label="Created" value={formatDate(createdTimestamp ?? null)} />
                  <InfoField
                    label="Last Updated"
                    value={
                      updatedTimestamp !== null ? (
                        <Stack spacing={0.5}>
                          <Typography variant="body1">{formatDate(updatedTimestamp ?? null)}</Typography>
                          <Typography variant="caption" color="text.secondary">
                            {formatRelativeTime(updatedTimestamp)}
                          </Typography>
                        </Stack>
                      ) : (
                        'Not yet updated'
                      )
                    }
                  />
                  <InfoField label="Duration" value={durationSeconds !== null ? formatDuration(durationSeconds) : '—'} />
                </Stack>
              </Grid>
            </Grid>

            {timelineEntries.length > 0 && (
              <Stack spacing={2}>
                <Divider />
                <Typography variant="subtitle2" color="text.secondary">
                  Timeline
                </Typography>
                <Stack spacing={2}>
                  {timelineEntries.map((entry, index) => (
                    <TimelineRow
                      key={entry.id}
                      entry={entry}
                      formattedTimestamp={formatDate(entry.timestamp)}
                      isLast={index === timelineEntries.length - 1}
                    />
                  ))}
                </Stack>
              </Stack>
            )}
          </Stack>
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
                startIcon={<LaunchIcon fontSize="small" />}
              >
                Open in Argo CD UI
              </Button>
            ) : (
              <Tooltip title="Argo CD URL is not configured for this environment.">
                <span>
                  <Button variant="outlined" disabled startIcon={<LaunchIcon fontSize="small" />}>
                    Open in Argo CD UI
                  </Button>
                </span>
              </Tooltip>
            )}
            {showRollbackButton && (
              <Tooltip title={rollbackTooltip} disableHoverListener={!rollbackTooltip}>
                <span>
                  <Button
                    variant="contained"
                    onClick={handleOpenConfirm}
                    disabled={rollbackDisabled}
                    startIcon={rollbackLoading ? <CircularProgress size={16} /> : undefined}
                  >
                    Rollback to this version
                  </Button>
                </span>
              </Tooltip>
            )}
          </Stack>
        </CardContent>
      </Card>

      {data.status_reason && (
        <Alert severity={descriptor.reasonSeverity} role="status" aria-live="polite">
          <Typography component="pre" sx={{ whiteSpace: 'pre-wrap', m: 0 }}>
            {data.status_reason}
          </Typography>
        </Alert>
      )}

      <Card>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            Images
          </Typography>
          <ImagesList images={displayedImages} hasAdditional={hasAdditionalImages} />
        </CardContent>
      </Card>
      {deployLock && (
        <Alert severity="error" role="status" aria-live="assertive">
          Deploy lock is active. Rollbacks are temporarily blocked.
        </Alert>
      )}
      <Dialog open={confirmOpen} onClose={handleCloseConfirm} aria-labelledby="rollback-dialog-title">
        <DialogTitle id="rollback-dialog-title">Rollback Confirmation</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Are you sure you want to rollback to this version? This will trigger a new deployment task.
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseConfirm} disabled={rollbackLoading}>
            Cancel
          </Button>
          <Button onClick={handleConfirmRollback} autoFocus disabled={rollbackLoading}>
            Yes
          </Button>
        </DialogActions>
      </Dialog>
    </Stack>
  );
};

interface InfoFieldProps {
  readonly label: string;
  readonly value: ReactNode;
}

/**
 * Displays a single piece of labeled metadata inside the task details view.
 */
const InfoField = ({ label, value }: InfoFieldProps) => (
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

/** Renders the project field, converting URLs into external links. */
const ProjectReference = ({ project }: { project?: string | null }) => {
  if (!project) {
    return <Typography variant="body1">—</Typography>;
  }

  const isUrl = project.startsWith('http://') || project.startsWith('https://');
  if (!isUrl) {
    return <Typography variant="body1">{project}</Typography>;
  }

  const label = project.replace(/^https?:\/\//, '').replace(/\/+$/, '');
  return (
    <Link href={project} target="_blank" rel="noopener noreferrer">
      {label}
    </Link>
  );
};

/** Visual row within the timeline stack with a connector to mimic the legacy UI flow. */
const TimelineRow = ({
  entry,
  isLast,
  formattedTimestamp,
}: {
  entry: TimelineEntry;
  formattedTimestamp: string;
  isLast: boolean;
}) => (
  <Stack direction="row" spacing={2}>
    <Box sx={{ position: 'relative', width: 24, display: 'flex', justifyContent: 'center' }}>
      <FiberManualRecordIcon color={entry.color === 'default' ? 'disabled' : entry.color} fontSize="small" />
      {!isLast && (
        <Box
          sx={theme => ({
            position: 'absolute',
            top: 18,
            width: 2,
            height: 'calc(100% - 18px)',
            backgroundColor: theme.palette.divider,
            borderRadius: 1,
          })}
        />
      )}
    </Box>
    <Stack spacing={0.25}>
      <Typography variant="subtitle2">{entry.label}</Typography>
      <Typography variant="body2" color="text.secondary">
        {formattedTimestamp}
      </Typography>
      <Typography variant="caption" color="text.secondary">
        {formatRelativeTime(entry.timestamp)}
      </Typography>
    </Stack>
  </Stack>
);

/** Displays container images using monospace references and chip-style tags. */
const ImagesList = ({
  images,
  hasAdditional,
}: {
  images: TaskStatus['images'];
  hasAdditional: boolean;
}) => {
  const list = images ?? [];
  if (!list.length) {
    return (
      <Typography variant="body2" color="text.secondary">
        No container images were reported for this task.
      </Typography>
    );
  }

  return (
    <Stack spacing={1.25}>
      {list.map(image => (
        <Stack
          key={`${image.image}:${image.tag}`}
          direction={{ xs: 'column', sm: 'row' }}
          spacing={1}
          alignItems={{ xs: 'flex-start', sm: 'center' }}
        >
          <Typography variant="body2" sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>
            {image.image}
          </Typography>
          <Chip label={image.tag} size="small" color="primary" variant="outlined" />
        </Stack>
      ))}
      {hasAdditional && (
        <Typography variant="caption" color="text.secondary">
          Showing the first {MAX_IMAGES_RENDERED} images.
        </Typography>
      )}
    </Stack>
  );
};
