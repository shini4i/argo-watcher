import { Alert, Snackbar } from '@mui/material';
import type { SxProps, Theme } from '@mui/material/styles';
import { useDeployLockState } from './useDeployLockState';
import { useArgocdUnreachable } from '../argocdStatus/useArgocdUnreachable';

const snackbarPosition: SxProps<Theme> = theme => ({
  '&.MuiSnackbar-root': {
    bottom: `calc(${theme.spacing(3)} + env(safe-area-inset-bottom))`,
    zIndex: theme.zIndex.snackbar,
  },
});

/** Displays a warning banner anchored to the bottom of the viewport when the deploy lock is active. */
export const DeployLockBanner = () => {
  const locked = useDeployLockState();
  const argocdUnreachable = useArgocdUnreachable();

  // An unreachable ArgoCD/state backend is the more severe condition and owns
  // the same bottom slot, so the deploy-lock warning yields to it.
  if (!locked || argocdUnreachable) {
    return null;
  }

  return (
    <Snackbar
      open
      anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      autoHideDuration={null}
      sx={snackbarPosition}
    >
      <Alert severity="warning" variant="filled" sx={{ minWidth: 320, alignItems: 'center' }}>
        <output aria-live="polite" style={{ display: 'block', width: '100%' }}>
          Deploy lock is active — privileged users must release the lock before triggering new deployments.
        </output>
      </Alert>
    </Snackbar>
  );
};
