import { useEffect, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import Alert from '@mui/material/Alert';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import { initializeAuth } from './authProvider';

interface AuthBootstrapProps {
  children: (keycloakEnabled: boolean) => ReactNode;
}

interface AuthState {
  status: 'loading' | 'ready' | 'error';
  keycloakEnabled: boolean;
  error?: string;
}

/**
 * Bootstrap component that initializes authentication before rendering the app.
 * Handles OAuth callback processing when returning from Keycloak.
 *
 * This component MUST wrap the React-admin Admin component to ensure
 * authentication is initialized before routing begins.
 */
export const AuthBootstrap = ({ children }: AuthBootstrapProps) => {
  const [state, setState] = useState<AuthState>({
    status: 'loading',
    keycloakEnabled: false,
  });

  const initialize = async () => {
    setState({ status: 'loading', keycloakEnabled: false });
    try {
      const result = await initializeAuth();
      setState({
        status: 'ready',
        keycloakEnabled: result.keycloakEnabled,
      });
    } catch (error) {
      setState({
        status: 'error',
        keycloakEnabled: false,
        error: error instanceof Error ? error.message : 'Authentication initialization failed',
      });
    }
  };

  useEffect(() => {
    initialize();
  }, []);

  if (state.status === 'loading') {
    return (
      <Box
        sx={{
          display: 'flex',
          flexDirection: 'column',
          minHeight: '100vh',
          alignItems: 'center',
          justifyContent: 'center',
          gap: 2,
        }}
      >
        <CircularProgress />
        <Typography variant="body2" color="text.secondary">
          Initializing...
        </Typography>
      </Box>
    );
  }

  if (state.status === 'error') {
    return (
      <Box
        sx={{
          display: 'flex',
          flexDirection: 'column',
          minHeight: '100vh',
          alignItems: 'center',
          justifyContent: 'center',
          p: 3,
        }}
      >
        <Alert
          severity="error"
          sx={{ maxWidth: 500, mb: 2 }}
          action={
            <Button color="inherit" size="small" onClick={initialize}>
              Retry
            </Button>
          }
        >
          {state.error}
        </Alert>
      </Box>
    );
  }

  return <>{children(state.keycloakEnabled)}</>;
};
