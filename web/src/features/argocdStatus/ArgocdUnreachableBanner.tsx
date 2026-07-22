import { Alert, Snackbar } from '@mui/material';
import type { SxProps, Theme } from '@mui/material/styles';
import { useArgocdStatus } from './ArgocdStatusProvider';
import type { ArgocdUnavailableReason } from './argocdStatusService';

// Anchored to the bottom for consistency with every other error surfaced by the
// app. It shares this slot with the deploy-lock banner, which yields to it (see
// DeployLockBanner), so the two never overlap.
const snackbarPosition: SxProps<Theme> = theme => ({
  '&.MuiSnackbar-root': {
    bottom: `calc(${theme.spacing(3)} + env(safe-area-inset-bottom))`,
    zIndex: theme.zIndex.snackbar,
  },
});

/**
 * Picks banner wording that names the exact unreachable subsystem. A null reason
 * (legacy signal without a cause) falls back to naming both, matching the
 * combined `both` message.
 */
const messageForReason = (reason: ArgocdUnavailableReason): string => {
  switch (reason) {
    case 'argocd':
      return 'argo-watcher cannot reach ArgoCD — deployments are not being processed. Check argo-watcher and ArgoCD connectivity.';
    case 'database':
      return 'argo-watcher cannot reach its state backend (database) — deployments are not being processed. Check database connectivity.';
    default:
      return 'argo-watcher cannot reach ArgoCD or its state backend — deployments are not being processed. Check argo-watcher, ArgoCD, and database connectivity.';
  }
};

/** Displays an error banner at the bottom of the viewport when ArgoCD or its state backend is unreachable. */
export const ArgocdUnreachableBanner = () => {
  const { available, reason } = useArgocdStatus();

  // Gate on availability, not reason: a legacy down signal can leave reason null
  // while still being a real outage, which must still show the (fallback) banner.
  if (available) {
    return null;
  }

  return (
    <Snackbar
      open
      anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      autoHideDuration={null}
      sx={snackbarPosition}
    >
      <Alert severity="error" variant="filled" sx={{ minWidth: 320, alignItems: 'center' }}>
        <output aria-live="assertive" style={{ display: 'block', width: '100%' }}>
          {messageForReason(reason)}
        </output>
      </Alert>
    </Snackbar>
  );
};
