import LightModeIcon from '@mui/icons-material/LightMode';
import NightlightIcon from '@mui/icons-material/Nightlight';
import LockIcon from '@mui/icons-material/Lock';
import LockOpenIcon from '@mui/icons-material/LockOpen';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import {
  Box,
  Button,
  CircularProgress,
  Divider,
  Drawer,
  IconButton,
  Stack,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNotify, usePermissions } from 'react-admin';
import { useThemeMode } from '../../theme';
import { httpClient } from '../../data/httpClient';
import { useDeployLock } from '../../features/deployLock/DeployLockProvider';
import { useKeycloakEnabled } from '../../shared/hooks/useKeycloakEnabled';
import { hasPrivilegedAccess } from '../../shared/utils/permissions';
import { useTimezone } from '../../shared/providers/TimezoneProvider';

interface ConfigDrawerProps {
  open: boolean;
  onClose: () => void;
  version: string;
}

type ConfigData = Record<string, unknown>;

/** Renders nested configuration values as strings for the drawer table. */
const renderValue = (value: unknown): string => {
  if (value === null) {
    return 'null';
  }

  if (typeof value === 'object') {
    try {
      return JSON.stringify(value, null, 2);
    } catch {
      return '[unserializable]';
    }
  }

  return String(value);
};

/**
 * Side drawer that surfaces runtime configuration, theme toggles, and deploy lock controls.
 */
export const ConfigDrawer = ({ open, onClose, version }: ConfigDrawerProps) => {
  const [config, setConfig] = useState<ConfigData | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lockUpdating, setLockUpdating] = useState(false);
  const { mode, toggleMode } = useThemeMode();
  const { timezone, setTimezone } = useTimezone();
  const browserZone = useMemo(() => Intl.DateTimeFormat().resolvedOptions().timeZone ?? 'local', []);
  const notify = useNotify();
  const { locked: deployLock, setLock, releaseLock } = useDeployLock();
  const keycloakEnabled = useKeycloakEnabled();
  const { permissions } = usePermissions();

  const groups: readonly string[] = (permissions as { groups?: string[] })?.groups ?? [];
  const privilegedGroups: readonly string[] =
    (permissions as { privilegedGroups?: string[] })?.privilegedGroups ?? [];
  const privileged = hasPrivilegedAccess(groups, privilegedGroups);
  const canToggleLock = keycloakEnabled ? privileged : true;

  const entries = useMemo(() => {
    if (!config) {
      return [];
    }
    return Object.entries(config);
  }, [config]);

  /** Renders the configuration section (loading, error, empty, or table). */
  const renderConfigContent = () => {
    if (loading) {
      return (
        <Stack alignItems="center" sx={{ py: 4 }}>
          <CircularProgress size={24} />
        </Stack>
      );
    }

    if (error) {
      return (
        <Typography variant="body2" color="error">
          {error}
        </Typography>
      );
    }

    if (entries.length === 0) {
      return (
        <Typography variant="body2" color="text.secondary">
          No configuration available.
        </Typography>
      );
    }

    return (
      <Box sx={{ flexGrow: 1, overflow: 'auto' }}>
        <Table size="small" stickyHeader>
          <TableHead>
            <TableRow>
              <TableCell sx={{ width: '35%' }}>Key</TableCell>
              <TableCell>Value</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {entries.map(([key, value]) => (
              <TableRow key={key} hover>
                <TableCell component="th" scope="row">
                  <Typography variant="body2" fontWeight={600}>
                    {key}
                  </Typography>
                </TableCell>
                <TableCell>
                  <Typography
                    variant="body2"
                    sx={{ whiteSpace: 'pre-wrap', fontFamily: theme => theme.typography.fontFamily }}
                  >
                    {renderValue(value)}
                  </Typography>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Box>
    );
  };

  useEffect(() => {
    if (!open) {
      return;
    }

    setLoading(true);
    setError(null);
    httpClient<ConfigData>('/api/v1/config')
      .then(response => {
        setConfig(response.data ?? {});
      })
      .catch(err => {
        const message = err instanceof Error ? err.message : 'Failed to load configuration';
        setError(message);
        notify(message, { type: 'warning' });
      })
      .finally(() => {
        setLoading(false);
      });
  }, [notify, open]);

  /** Copies the current config object to the clipboard in JSON form. */
  const handleCopy = useCallback(async () => {
    if (!config) {
      return;
    }

    const text = JSON.stringify(config, null, 2);
    try {
      if (!navigator.clipboard?.writeText) {
        throw new Error('Clipboard API unavailable in this environment.');
      }
      await navigator.clipboard.writeText(text);
      notify('Configuration copied to clipboard.', { type: 'info' });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Copy failed';
      notify(message, { type: 'warning' });
    }
  }, [config, notify]);

  /** Toggles the deploy lock via the REST API and surfaces user feedback. */
  const handleDeployLockToggle = useCallback(async () => {
    if (!canToggleLock) {
      return;
    }

    setLockUpdating(true);
    try {
      if (deployLock) {
        await releaseLock();
        notify('Deploy lock released.', { type: 'info' });
      } else {
        await setLock();
        notify('Deploy lock enabled.', { type: 'info' });
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to update deploy lock.';
      notify(message, { type: 'error' });
    } finally {
      setLockUpdating(false);
    }
  }, [canToggleLock, deployLock, notify, releaseLock, setLock]);

  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
      PaperProps={{ sx: { width: { xs: '100%', sm: 420 }, p: { xs: 2, sm: 3 } } }}
      aria-label="Workspace configuration drawer"
    >
      <Stack spacing={2} sx={{ height: '100%' }}>
        <Stack spacing={2} sx={{ flexGrow: 1 }}>
          <Stack direction="row" alignItems="center" justifyContent="space-between">
            <Typography variant="h6">Workspace Controls</Typography>
            <Typography variant="body2" color="text.secondary">
              v{version}
            </Typography>
          </Stack>

          <Stack spacing={1.5} component="section" aria-labelledby="drawer-appearance">
            <Typography variant="subtitle2" color="text.secondary">
              <span id="drawer-appearance">Appearance</span>
            </Typography>
            <Stack direction="row" alignItems="center" justifyContent="space-between">
            <Stack direction="row" spacing={1} alignItems="center">
              {mode === 'light' ? <LightModeIcon fontSize="small" /> : <NightlightIcon fontSize="small" />}
              <Typography variant="body2">Theme mode</Typography>
            </Stack>
            <Button variant="outlined" size="small" onClick={toggleMode}>
              Switch to {mode === 'light' ? 'dark' : 'light'}
            </Button>
          </Stack>
          <Stack direction="row" alignItems="center" justifyContent="space-between">
            <Stack direction="row" spacing={1} alignItems="center">
              <Typography variant="body2">Timezone</Typography>
            </Stack>
            <ToggleButtonGroup
              size="small"
              exclusive
              value={timezone}
              onChange={(_event, value) => value && setTimezone(value)}
              aria-label="Timezone selection"
            >
              <ToggleButton value="local">Local ({browserZone})</ToggleButton>
              <ToggleButton value="utc">UTC</ToggleButton>
            </ToggleButtonGroup>
          </Stack>
          </Stack>

          <Divider />

          <Stack spacing={1.5} component="section" aria-labelledby="drawer-lock">
            <Typography variant="subtitle2" color="text.secondary">
              <span id="drawer-lock">Deploy Lock</span>
            </Typography>
          <Stack direction="row" alignItems="center" justifyContent="space-between">
            <Stack direction="row" spacing={1} alignItems="center">
              {deployLock ? <LockIcon fontSize="small" /> : <LockOpenIcon fontSize="small" />}
              <Typography variant="body2">
                {deployLock ? 'Lock engaged' : 'Lock released'}
              </Typography>
            </Stack>
            <Switch
              checked={deployLock}
              onChange={handleDeployLockToggle}
              disabled={!canToggleLock || lockUpdating}
              inputProps={{ 'aria-label': 'Toggle deploy lock' }}
            />
          </Stack>
          {!canToggleLock && (
            <Typography variant="body2" color="text.secondary">
              Deploy lock requires privileged access.
            </Typography>
          )}
          </Stack>

          {(!keycloakEnabled || privileged) && (
            <>
              <Divider />

            <Stack direction="row" alignItems="center" justifyContent="space-between" component="section">
              <Typography variant="subtitle2" color="text.secondary">
                Backend Configuration
              </Typography>
              <IconButton size="small" onClick={handleCopy} aria-label="Copy configuration" disableRipple>
                <ContentCopyIcon fontSize="inherit" />
              </IconButton>
            </Stack>

            {renderConfigContent()}
          </>
          )}
        </Stack>

        <Box sx={{ textAlign: 'right' }}>
          <Button onClick={onClose}>Close</Button>
        </Box>
        <Box
          sx={{
            mt: 'auto',
            pt: 1.5,
            borderTop: theme => `1px solid ${theme.palette.divider}`,
            textAlign: 'center',
          }}
        >
          <Typography variant="caption" color="text.secondary">
            © 2022 - {new Date().getFullYear()} Vadim Gedz
          </Typography>
        </Box>
      </Stack>
    </Drawer>
  );
};
