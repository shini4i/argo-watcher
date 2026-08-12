import { Alert, AlertTitle, Box, Button, Chip, Link, Stack, Typography } from '@mui/material';
import { ThemeProvider, alpha, keyframes } from '@mui/material/styles';
import type { AuthFailure } from '../auth/authFailure';
import logoUrl from '../assets/logo.png';
import { darkTheme } from '../theme';
import { tokens } from '../theme/tokens';

const DOT_DELAYS_MS = [0, 160, 320];

const bounce = keyframes`
  0%, 80%, 100% { transform: translateY(0); opacity: 0.3; }
  40% { transform: translateY(-6px); opacity: 1; }
`;

interface AppSplashProps {
  /**
   * Status line beneath the logo. The caller starts with a neutral message and
   * narrows it once the server configuration says a sign-in is under way, so an
   * auth-less deployment is never told it is signing in.
   */
  readonly message: string;
  /**
   * Authentication failure to show beneath the panel. Its presence means the
   * sign-in is over and will not be retried on its own, so the progress dots are
   * dropped.
   */
  readonly error?: AuthFailure;
  /**
   * Starts a fresh sign-in attempt. Omit it to render the failure without a way
   * to retry (nothing the user can do from the browser will help).
   */
  readonly onRetry?: () => void;
}

/**
 * Full-viewport loading screen shown while authentication is resolved, before
 * the React-admin tree mounts.
 *
 * It is rendered outside `AppProviders` on purpose — the deploy-lock and
 * ArgoCD-status providers start polling the API on mount, which must not happen
 * before a token exists — so it brings its own theme and issues no requests.
 *
 * It always renders dark, whichever mode the user picked: the logo is white
 * artwork on transparency, so a light variant would need a second set of colours
 * for every element and a plate behind the logo.
 */
export const AppSplash = ({ message, error, onRetry }: AppSplashProps) => (
  <ThemeProvider theme={darkTheme}>
    <Box
      data-testid="app-splash"
      sx={{
        position: 'fixed',
        inset: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        backgroundColor: tokens.canvasDark,
        backgroundImage: tokens.appBarGradientDark,
      }}
    >
      {/* Centred, never stretched: the panel keeps the width of its own contents
          whether or not a failure is shown, so the error box appearing below it
          does not resize the logo card. */}
      <Stack
        data-testid="app-splash-stack"
        spacing={2.5}
        sx={{ alignItems: 'center', width: '100%', maxWidth: 560, px: 2 }}
      >
        <Stack
          data-testid="app-splash-panel"
          spacing={1.5}
          sx={theme => ({
            alignItems: 'center',
            // Set explicitly: text colour otherwise inherits from the document
            // body, which CssBaseline paints for the user's chosen mode — light
            // mode would put near-black text on this dark panel.
            color: theme.palette.text.primary,
            px: 5,
            py: 4,
            borderRadius: `${tokens.radiusLg}px`,
            border: `1px solid ${theme.palette.divider}`,
            backgroundColor: alpha(theme.palette.background.paper, 0.6),
            boxShadow: theme.shadows[8],
          })}
        >
          {/* Decorative: the product name follows as text. */}
          <Box component="img" src={logoUrl} alt="" sx={{ width: 56, height: 'auto' }} />
          <Typography sx={{ fontSize: 16, fontWeight: 600 }}>Argo Watcher</Typography>
          {/* Only the status line is a live region, so assistive technology
              announces the message rather than the whole panel. */}
          <Typography
            component="output"
            aria-live="polite"
            color="text.secondary"
            sx={{ fontSize: 13 }}
          >
            {message}
          </Typography>
          {/* Dropped once a failure is shown: the sign-in is over and nothing is
              waiting on the provider, so an animation would promise a retry that
              is not coming. */}
          {!error && (
            <Stack direction="row" spacing={0.75} sx={{ mt: 0.5 }}>
              {DOT_DELAYS_MS.map(delay => (
                <Box
                  key={delay}
                  data-testid="app-splash-dot"
                  sx={theme => ({
                    width: 8,
                    height: 8,
                    borderRadius: '50%',
                    backgroundColor: theme.palette.primary.main,
                    animation: `${bounce} 1.4s ease-in-out infinite`,
                    animationDelay: `${delay}ms`,
                    '@media (prefers-reduced-motion: reduce)': {
                      animation: 'none',
                      opacity: 0.6,
                    },
                  })}
                />
              ))}
            </Stack>
          )}
        </Stack>
        {error && (
          <Alert
            data-testid="app-splash-error"
            severity="error"
            variant="outlined"
            aria-live="assertive"
            sx={theme => ({
              width: '100%',
              // Explicit: the splash renders without CssBaseline, so the global
              // border-box rule is absent and the alert's padding would push it
              // wider than the panel above it.
              boxSizing: 'border-box',
              textAlign: 'left',
              backgroundColor: alpha(theme.palette.background.paper, 0.6),
            })}
          >
            <AlertTitle sx={{ mb: 1 }}>{error.title}</AlertTitle>
            <Stack spacing={1} sx={{ alignItems: 'flex-start' }}>
              {error.code && (
                <Chip
                  label={error.code}
                  size="small"
                  color="error"
                  variant="outlined"
                  sx={{ fontFamily: 'monospace', maxWidth: '100%' }}
                />
              )}
              {error.detail && (
                <Typography sx={{ fontSize: 13, overflowWrap: 'anywhere' }}>
                  {error.detail}
                </Typography>
              )}
              {error.hint && (
                <Typography color="text.secondary" sx={{ fontSize: 13 }}>
                  {error.hint}
                </Typography>
              )}
              {/* Provider-supplied target: withhold the opener and the referrer. */}
              {error.uri && (
                <Link
                  href={error.uri}
                  target="_blank"
                  rel="noopener noreferrer"
                  color="inherit"
                  sx={{ fontSize: 13 }}
                >
                  More information
                </Link>
              )}
              {onRetry && (
                <Button variant="outlined" color="inherit" size="small" onClick={onRetry}>
                  Try again
                </Button>
              )}
            </Stack>
          </Alert>
        )}
      </Stack>
    </Box>
  </ThemeProvider>
);
