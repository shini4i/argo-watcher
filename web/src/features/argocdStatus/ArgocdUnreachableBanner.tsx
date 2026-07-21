import { Alert, Snackbar } from '@mui/material';
import type { SxProps, Theme } from '@mui/material/styles';
import { useArgocdUnreachable } from './useArgocdUnreachable';

// Anchored to the top so it does not overlap the bottom-anchored deploy-lock
// banner when both are visible.
const snackbarPosition: SxProps<Theme> = theme => ({
  '&.MuiSnackbar-root': {
    top: `calc(${theme.spacing(3)} + env(safe-area-inset-top))`,
    zIndex: theme.zIndex.snackbar,
  },
});

/** Displays an error banner at the top of the viewport when ArgoCD is unreachable. */
export const ArgocdUnreachableBanner = () => {
  const unreachable = useArgocdUnreachable();

  if (!unreachable) {
    return null;
  }

  return (
    <Snackbar
      open
      anchorOrigin={{ vertical: 'top', horizontal: 'center' }}
      autoHideDuration={null}
      sx={snackbarPosition}
    >
      <Alert severity="error" variant="filled" sx={{ minWidth: 320, alignItems: 'center' }}>
        <output aria-live="assertive" style={{ display: 'block', width: '100%' }}>
          {/* The underlying signal also trips when the state backend is down, so
              the wording names both causes rather than blaming ArgoCD alone. */}
          argo-watcher cannot reach ArgoCD or its state backend — deployments are not being processed. Check argo-watcher, ArgoCD, and database connectivity.
        </output>
      </Alert>
    </Snackbar>
  );
};
