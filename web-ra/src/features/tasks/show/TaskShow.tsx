import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import FiberManualRecordIcon from '@mui/icons-material/FiberManualRecord';
import RefreshIcon from '@mui/icons-material/Refresh';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Divider,
  Grid,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
} from '@mui/material';
import { Fragment, useCallback, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { useGetIdentity, useGetOne, useNotify, usePermissions } from 'react-admin';
import { useNavigate, useParams } from 'react-router-dom';
import type { TaskStatus } from '../../../data/types';
import { formatDateTime, formatDuration, formatRelativeTime } from '../../../shared/utils/time';
import { describeTaskStatus } from '../utils/statusPresentation';
import { useDeployLockState } from '../../deployLock/useDeployLockState';
import { useKeycloakEnabled } from '../../../shared/hooks/useKeycloakEnabled';
import { hasPrivilegedAccess } from '../../../shared/utils/permissions';
import { httpClient } from '../../../data/httpClient';
import { getAccessToken } from '../../../auth/tokenStore';

interface TimelineEntry {
  readonly id: string;
  readonly label: string;
  readonly timestamp: number;
  readonly color: 'default' | 'primary' | 'secondary' | 'error' | 'info' | 'success' | 'warning';
}

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

/** Displays the task detail screen at `/task/:id` mirroring the legacy Task View experience. */
export const TaskShow = () => {
  const { id } = useParams<{ id: string }>();
  const notify = useNotify();
  const navigate = useNavigate();
  const deployLock = useDeployLockState();
  const keycloakEnabled = useKeycloakEnabled();
  const { permissions } = usePermissions();
  const { data: identity } = useGetIdentity();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [rollbackLoading, setRollbackLoading] = useState(false);

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

  const status = data?.status ?? null;
  const identityEmail = identity?.email ?? '';
  const groups: readonly string[] = (permissions as { groups?: string[] })?.groups ?? [];
  const privilegedGroups: readonly string[] =
    (permissions as { privilegedGroups?: string[] })?.privilegedGroups ?? [];
  const userIsPrivileged = hasPrivilegedAccess(groups, privilegedGroups);
  const showRollbackButton = keycloakEnabled && userIsPrivileged;

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

  const descriptor = describeTaskStatus(data.status);
  const createdTimestamp = normalizeTimestamp(data.created);
  const updatedTimestamp = normalizeTimestamp(data.updated);

  const durationSeconds = useMemo(() => {
    if (createdTimestamp === null) {
      return null;
    }
    const effectiveUpdated =
      data.status === 'in progress' || updatedTimestamp === null
        ? Math.floor(Date.now() / 1000)
        : updatedTimestamp;
    return Math.max(0, effectiveUpdated - createdTimestamp);
  }, [createdTimestamp, updatedTimestamp, data.status]);

  const timelineEntries = useMemo(() => {
    const entries: TimelineEntry[] = [];
    if (createdTimestamp !== null) {
      entries.push({
        id: 'created',
        label: 'Created',
        timestamp: createdTimestamp,
        color: 'info',
      });
    }
    if (updatedTimestamp !== null) {
      entries.push({
        id: 'status',
        label: descriptor.label,
        timestamp: updatedTimestamp,
        color: descriptor.timelineDotColor,
      });
    }
    return entries;
  }, [createdTimestamp, descriptor.label, descriptor.timelineDotColor, updatedTimestamp]);

  const images = (data.images ?? []).slice(0, MAX_IMAGES_RENDERED);

  const rollbackState = computeRollbackState(status, deployLock, Boolean(identityEmail));
  const rollbackDisabled = rollbackState.disabled || rollbackLoading;
  const rollbackTooltip = rollbackDisabled && !rollbackLoading ? rollbackState.message : '';

  return (
    <Stack spacing={3} sx={{ mt: { xs: 1.5, sm: 2 }, px: { xs: 1, md: 0 } }}>
      <Stack
        direction={{ xs: 'column', sm: 'row' }}
        justifyContent={{ xs: 'flex-start', sm: 'space-between' }}
        alignItems={{ xs: 'stretch', sm: 'center' }}
        spacing={1.5}
        sx={{ width: '100%', rowGap: 1.5 }}
      >
        <Stack direction="row" spacing={1} alignItems="center" sx={{ flexWrap: { xs: 'wrap', sm: 'nowrap' } }}>
          <Button onClick={handleBack} startIcon={<ArrowBackIcon />} variant="text">
            Back
          </Button>
          <Button onClick={handleRefresh} startIcon={<RefreshIcon fontSize="small" />} variant="outlined">
            Refresh
          </Button>
        </Stack>
        <Stack
          direction="row"
          spacing={1}
          alignItems="center"
          justifyContent={{ xs: 'flex-start', sm: 'flex-end' }}
          sx={{ flexWrap: { xs: 'wrap', sm: 'nowrap' } }}
        >
          <Chip label={descriptor.label} color={descriptor.chipColor} size="medium" icon={descriptor.icon as ReactNode} />
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
      </Stack>

      <Card>
        <CardContent>
          <Grid container spacing={3}>
            <Grid item xs={12} md={6}>
              <Typography variant="h5" gutterBottom>
                {data.app ?? 'Unknown application'}
              </Typography>
              <Stack spacing={1}>
                <InfoField label="Task ID" value={data.id} />
                <InfoField label="Project" value={data.project ?? '—'} />
                <InfoField label="Author" value={data.author ?? '—'} />
              </Stack>
            </Grid>
            <Grid item xs={12} md={6}>
              <Stack spacing={1}>
                <InfoField label="Created" value={formatDateTime(createdTimestamp)} />
                <InfoField label="Last Updated" value={updatedTimestamp ? formatRelativeTime(updatedTimestamp) : 'Not yet updated'} />
                <InfoField label="Duration" value={durationSeconds !== null ? formatDuration(durationSeconds) : '—'} />
              </Stack>
            </Grid>
          </Grid>
        </CardContent>
      </Card>

      {timelineEntries.length > 0 && (
        <Card>
          <CardContent>
            <Typography variant="h6" gutterBottom>
              Timeline
            </Typography>
            <List sx={{ '& .MuiListItem-root': { alignItems: 'flex-start' } }}>
              {timelineEntries.map((entry, index) => (
                <Fragment key={entry.id}>
                  <ListItem disableGutters>
                    <ListItemIcon sx={{ minWidth: 32 }}>
                      <FiberManualRecordIcon
                        color={entry.color === 'default' ? 'disabled' : entry.color}
                        fontSize="small"
                      />
                    </ListItemIcon>
                    <ListItemText
                      primary={entry.label}
                      secondary={formatDateTime(entry.timestamp)}
                      primaryTypographyProps={{ variant: 'body1' }}
                      secondaryTypographyProps={{ variant: 'body2', color: 'text.secondary' }}
                    />
                  </ListItem>
                  {index < timelineEntries.length - 1 && <Divider component="li" variant="inset" />}
                </Fragment>
              ))}
            </List>
          </CardContent>
        </Card>
      )}

      {data.status_reason && (
        <Alert severity={descriptor.reasonSeverity}>
          <Typography component="pre" sx={{ whiteSpace: 'pre-wrap', m: 0 }}>
            {data.status_reason}
          </Typography>
        </Alert>
      )}

      {images.length > 0 && (
        <Card>
          <CardContent>
            <Typography variant="h6" gutterBottom>
              Images
            </Typography>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Image</TableCell>
                  <TableCell>Tag</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {images.map(image => (
                  <TableRow key={`${image.image}:${image.tag}`}>
                    <TableCell>{image.image}</TableCell>
                    <TableCell>{image.tag}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
      {deployLock && (
        <Alert severity="error">
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
