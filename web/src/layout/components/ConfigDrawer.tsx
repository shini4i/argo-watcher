import LightModeIcon from '@mui/icons-material/LightMode';
import NightlightIcon from '@mui/icons-material/Nightlight';
import LockIcon from '@mui/icons-material/Lock';
import LockOpenIcon from '@mui/icons-material/LockOpen';
import {
  Box,
  Button,
  Divider,
  Drawer,
  Stack,
  Switch,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import { useCallback, useMemo, useState } from 'react';
import { useNotify, usePermissions } from 'react-admin';
import { useThemeMode } from '../../theme';
import { useDeployLock } from '../../features/deployLock/DeployLockProvider';
import { useKeycloakEnabled } from '../../shared/hooks/useKeycloakEnabled';
import { hasPrivilegedAccess } from '../../shared/utils/permissions';
import { useTimezone } from '../../shared/providers/TimezoneProvider';

interface ConfigDrawerProps {
  open: boolean;
  onClose: () => void;
  version: string;
}

/**
 * Side drawer that surfaces appearance, timezone, and deploy lock controls.
 */
export const ConfigDrawer = ({ open, onClose, version }: ConfigDrawerProps) => {
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
  // Default-deny while keycloakEnabled is unknown (null = config still loading
  // or the /api/v1/config request failed) so a non-privileged user cannot
  // toggle the lock during the brief startup window or on a transient error.
  const canToggleLock =
    keycloakEnabled === false || (keycloakEnabled === true && privileged);
  const lockHelperText =
    keycloakEnabled === null
      ? 'Checking permissions…'
      : keycloakEnabled === true && !privileged
        ? 'Deploy lock requires privileged access.'
        : null;

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
            {lockHelperText && (
              <Typography variant="body2" color="text.secondary">
                {lockHelperText}
              </Typography>
            )}
          </Stack>
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
