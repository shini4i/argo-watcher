import { useState } from 'react';
import { Login, useLogin, useNotify } from 'react-admin';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import CircularProgress from '@mui/material/CircularProgress';
import Typography from '@mui/material/Typography';

/**
 * Custom login page for Keycloak authentication using react-admin's Login wrapper.
 * Displays a "Login with Keycloak" button instead of the default username/password form.
 * Only shown when Keycloak authentication is enabled.
 */
export const LoginPage = () => {
  const login = useLogin();
  const notify = useNotify();
  const [loading, setLoading] = useState(false);

  const handleLogin = async () => {
    setLoading(true);
    try {
      // Pass current location to preserve deep links after login
      await login({ redirectTo: window.location.pathname + window.location.search });
    } catch (error) {
      notify('Login failed. Please try again.', { type: 'error' });
      setLoading(false);
    }
  };

  return (
    <Login>
      <Box sx={{ textAlign: 'center', py: 2 }}>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
          Sign in to monitor your ArgoCD deployments
        </Typography>
        <Button
          variant="contained"
          color="primary"
          onClick={handleLogin}
          disabled={loading}
          fullWidth
          size="large"
          sx={{
            py: 1.5,
            textTransform: 'none',
            fontSize: '1rem',
          }}
        >
          {loading ? <CircularProgress size={24} color="inherit" /> : 'Login with Keycloak'}
        </Button>
      </Box>
    </Login>
  );
};
