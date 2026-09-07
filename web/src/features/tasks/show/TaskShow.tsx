import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import RefreshIcon from '@mui/icons-material/Refresh';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Stack,
  Typography,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useGetIdentity, useGetOne, useNotify, usePermissions } from 'react-admin';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import type { TaskStatus } from '../../../data/types';
import { formatDuration } from '../../../shared/utils/time';
import { describeTaskStatus, isFailedStatus } from '../utils/statusPresentation';
import { summariseFailure } from '../utils/failureReason';
import { TaskHeader } from './components/TaskHeader';
import { FailureReasonPanel } from './components/FailureReasonPanel';
import { TaskLifecycle } from './components/TaskLifecycle';
import { TaskDeploymentCard, TaskImagesCard } from './components/TaskInfoCards';
import { usePreviousDeploy } from './usePreviousDeploy';
import { useDeployLockState } from '../../deployLock/useDeployLockState';
import { useOidcEnabled } from '../../../shared/hooks/useOidcEnabled';
import { getBrowserWindow, hasPrivilegedAccess, normalizeError } from '../../../shared/utils';
import { httpClient } from '../../../data/httpClient';
import { describeReadFailure } from '../../../data/readFailure';
import { getAccessToken } from '../../../auth/tokenStore';
import { useTimezone } from '../../../shared/providers/TimezoneProvider';

const CLOCK_FORMAT: Intl.DateTimeFormatOptions = {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
};

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

interface RollbackState {
  disabled: boolean;
  message: string;
}

interface ConfigResponse {
  argo_cd_url_alias?: string;
  argo_cd_url?: string;
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

const buildArgoCdUrl = (config: ConfigResponse | null, app?: string | null): string | null => {
  if (!config || !app) {
    return null;
  }

  const base = config.argo_cd_url_alias || config.argo_cd_url;
  if (typeof base !== 'string' || base.length === 0) {
    return null;
  }

  try {
    // Assigning the pathname keeps the route out of a query or fragment the
    // configured URL may carry.
    const url = new URL(base, window.location.href);
    const segments = url.pathname.split('/').filter(Boolean);
    url.pathname = `/${[...segments, 'applications', app].join('/')}`;
    return url.toString();
  } catch {
    return null;
  }
};

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

/** Phrases the elapsed span as the outcome it belongs to, e.g. "failed after 4m". */
const describeElapsed = (
  status: string | null,
  terminalLabel: string,
  durationSeconds: number,
): string => {
  const verb = status === 'in progress' ? 'running for' : `${terminalLabel.toLowerCase()} after`;
  return `${verb} ${formatDuration(durationSeconds)}`;
};


/** Routed at `/task/:id`. */
export const TaskShow = () => {
  const { id } = useParams<{ id: string }>();
  const notify = useNotify();
  const navigate = useNavigate();
  const location = useLocation();
  const deployLock = useDeployLockState();
  const oidcEnabled = useOidcEnabled();
  const { permissions } = usePermissions();
  const { data: identity } = useGetIdentity();
  const { formatDate } = useTimezone();
  const theme = useTheme();
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

  // A 404 with nothing loaded is the not-found card below, which names the id itself, so
  // it needs no toast. Every other failure — a 404 over a loaded task included — is named.
  const isMissingTask = isError && !data && normalizeError(error).status === 404;

  // Memoised because it drives a notify effect that must not fire per render.
  const readFailure = useMemo(
    () => (isError && error && !isMissingTask ? describeReadFailure(error) : null),
    [isError, error, isMissingTask],
  );

  useEffect(() => {
    if (readFailure) {
      notify(readFailure.title, { type: 'error' });
    }
  }, [readFailure, notify]);

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
  const showRollbackButton = oidcEnabled && userIsPrivileged;
  const descriptor = describeTaskStatus(status);
  const createdTimestamp = normalizeTimestamp(data?.created);
  const updatedTimestamp = normalizeTimestamp(data?.updated);
  const durationSeconds = computeDurationSeconds(status, createdTimestamp, updatedTimestamp);
  const failureSummary = summariseFailure(data?.status_reason);
  const previousDeploy = usePreviousDeploy(data?.app, data?.id, createdTimestamp);
  const terminalColor =
    theme.palette.mode === 'dark' ? descriptor.pillFgDark : descriptor.pillFg;
  const subLine = [
    data?.author,
    createdTimestamp === null ? null : `started ${formatDate(createdTimestamp, CLOCK_FORMAT)}`,
    durationSeconds === null
      ? null
      : describeElapsed(status, descriptor.displayLabel, durationSeconds),
  ]
    .filter(Boolean)
    .join(' · ');
  const argoCdUrl = buildArgoCdUrl(configData, data?.app);
  const rollbackState = computeRollbackState(status, deployLock, Boolean(identityEmail));
  const rollbackDisabled = rollbackState.disabled || rollbackLoading;
  const rollbackTooltip = rollbackDisabled && !rollbackLoading ? rollbackState.message : '';

  useEffect(() => {
    if (!id || status !== 'in progress') {
      return;
    }

    const browserWindow = getBrowserWindow();
    if (!browserWindow) {
      return undefined;
    }

    const intervalId = browserWindow.setInterval(() => {
      const result = refetch();
      if (result && typeof result.catch === 'function') {
        result.catch(error => {
          if (import.meta.env.DEV) {
            console.warn('TaskShow refetch failed', error);
          }
        });
      }
    }, 10_000);

    return () => browserWindow.clearInterval(intervalId);
  }, [id, refetch, status]);

  const handleBack = useCallback(() => {
    // A `default` key means this is the router's first entry (direct task URL):
    // there is no in-app screen behind it, and stepping out of the SPA forces a
    // fresh OIDC sign-in that lands the user right back here.
    if (location.key === 'default') {
      navigate('/tasks');
      return;
    }
    navigate(-1);
  }, [location.key, navigate]);

  const handleRefresh = useCallback(() => {
    const result = refetch();
    if (result && typeof result.catch === 'function') {
      result.catch(error => {
        if (import.meta.env.DEV) {
          console.warn('TaskShow refresh failed', error);
        }
      });
    }
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
      if (oidcEnabled && token) {
        headers['Oidc-Authorization'] = `Bearer ${token}`;
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
  }, [data, identityEmail, oidcEnabled, navigate, notify]);

  if (!id) {
    return (
      <Card>
        <CardContent>
          <Typography variant="h6">Task not specified</Typography>
          <Typography variant="body2" sx={{
            color: 'text.secondary'
          }}>
            The requested task cannot be located because the identifier is missing from the URL.
          </Typography>
        </CardContent>
      </Card>
    );
  }

  if (isLoading) {
    return (
      <Stack
        spacing={2}
        sx={{
          alignItems: 'center',
          py: 6
        }}>
        <CircularProgress />
        <Typography variant="body1">Loading task details…</Typography>
      </Stack>
    );
  }

  // Only when there is nothing to show. A poll failure over a loaded task keeps the
  // detail view and reports itself through the toast, as the task list does.
  if (readFailure && !data) {
    return (
      <Card>
        <CardContent>
          <Typography variant="h6">{readFailure.title}</Typography>
          <Typography variant="body2" sx={{ color: 'text.secondary' }}>
            {readFailure.detail}
          </Typography>
          <Typography variant="body2" sx={{ color: 'text.secondary', mt: 1 }}>
            {readFailure.hint}
          </Typography>
        </CardContent>
      </Card>
    );
  }

  if (!data) {
    return (
      <Card>
        <CardContent>
          <Typography variant="h6">Task not found</Typography>
          <Typography variant="body2" sx={{
            color: 'text.secondary'
          }}>
            The task with identifier <strong>{id}</strong> could not be located.
          </Typography>
        </CardContent>
      </Card>
    );
  }

  return (
    <Stack spacing={2.5} sx={{ mt: { xs: 1.5, sm: 2 }, px: { xs: 1, md: 0 } }}>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
        <Button onClick={handleBack} startIcon={<ArrowBackIcon />} variant="text" size="small">
          Back
        </Button>
        <Button
          onClick={handleRefresh}
          startIcon={<RefreshIcon fontSize="small" />}
          variant="outlined"
          size="small"
        >
          Refresh
        </Button>
      </Stack>

      <TaskHeader
        task={data}
        subLine={subLine}
        argoCdUrl={argoCdUrl}
        showRedeploy={showRollbackButton}
        redeployDisabled={rollbackDisabled}
        redeployTooltip={rollbackTooltip}
        redeployLoading={rollbackLoading}
        onRedeploy={handleOpenConfirm}
      />

      {failureSummary && (
        <FailureReasonPanel
          summary={failureSummary}
          tone={isFailedStatus(status) ? 'error' : 'neutral'}
        />
      )}

      {createdTimestamp !== null && (
        <TaskLifecycle
          createdLabel={formatDate(createdTimestamp, CLOCK_FORMAT)}
          created={createdTimestamp}
          terminalLabel={descriptor.displayLabel}
          terminalLabelColor={terminalColor}
          terminalTime={updatedTimestamp === null ? undefined : formatDate(updatedTimestamp, CLOCK_FORMAT)}
          terminalTimestamp={updatedTimestamp}
          durationSeconds={durationSeconds}
          isRunning={status === 'in progress'}
        />
      )}

      <Box
        sx={{
          display: 'grid',
          gap: 2.5,
          gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' },
        }}
      >
        <TaskDeploymentCard task={data} previousDeploy={previousDeploy} />
        <TaskImagesCard images={data.images} />
      </Box>

      {deployLock && (
        <Alert severity="error">
          <output aria-live="assertive" style={{ display: 'block' }}>
            Deploy lock is active. Rollbacks are temporarily blocked.
          </output>
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
