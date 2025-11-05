import React, { useCallback, useContext, useEffect, useState } from 'react';
import {
  Box,
  Button,
  CircularProgress,
  Drawer,
  Paper,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import { alpha, styled } from '@mui/material/styles';

import { fetchConfig } from '../Services/Data';
import { releaseDeployLock, setDeployLock, useDeployLock } from '../Services/DeployLockHandler';
import { AuthContext } from '../Services/Auth';
import { useThemeMode } from '../ThemeModeContext';

interface ConfigData {
  [key: string]: any;
}

interface SidebarProps {
  open: boolean;
  onClose: () => void;
}

const ControlCard = styled(Paper)(({ theme }) => ({
  padding: theme.spacing(1.5),
  borderRadius: theme.shape.borderRadius * 1.6,
  border: `1px solid ${alpha(theme.palette.divider, theme.palette.mode === 'light' ? 0.35 : 0.55)}`,
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  gap: theme.spacing(1.5),
  width: '100%',
  backgroundColor:
    theme.palette.mode === 'light'
      ? alpha(theme.palette.background.paper, 0.88)
      : alpha(theme.palette.background.paper, 0.58),
  boxShadow: theme.palette.mode === 'light'
    ? '0 10px 18px rgba(15, 23, 42, 0.08)'
    : '0 12px 20px rgba(8, 11, 26, 0.3)',
  backdropFilter: 'blur(6px)',
}));

const BaseSwitch = styled(Switch)(({ theme }) => ({
  width: 48,
  height: 26,
  padding: 0,
  '& .MuiSwitch-switchBase': {
    padding: 2,
    transform: 'translateX(2px)',
    '&.Mui-checked': {
      transform: 'translateX(20px)',
      color: '#fff',
      '& + .MuiSwitch-track': {
        opacity: 1,
      },
    },
  },
  '& .MuiSwitch-thumb': {
    width: 20,
    height: 20,
    borderRadius: 12,
    boxShadow: '0 4px 8px rgba(15, 23, 42, 0.28)',
    backgroundColor: theme.palette.mode === 'light' ? '#ffffff' : '#0f172a',
    position: 'relative',
    transition: theme.transitions.create(['background-color'], {
      duration: theme.transitions.duration.shortest,
    }),
  },
  '& .MuiSwitch-track': {
    borderRadius: 32,
    backgroundColor:
      theme.palette.mode === 'light' ? 'rgba(15, 23, 42, 0.18)' : 'rgba(148, 163, 184, 0.35)',
    opacity: 1,
    transition: theme.transitions.create(['background-color'], {
      duration: theme.transitions.duration.shorter,
    }),
  },
}));

const ThemeSwitch = styled(BaseSwitch)(({ theme }) => ({
  '& .MuiSwitch-track': {
    background:
      theme.palette.mode === 'light'
        ? 'linear-gradient(135deg, #fde68a 0%, #f59e0b 100%)'
        : 'linear-gradient(135deg, #312e81 0%, #1f2937 100%)',
  },
  '& .MuiSwitch-switchBase.Mui-checked + .MuiSwitch-track': {
    background:
      theme.palette.mode === 'light'
        ? 'linear-gradient(135deg, #4338ca 0%, #1f2937 100%)'
        : 'linear-gradient(135deg, #4f46e5 0%, #0f172a 100%)',
  },
  '& .MuiSwitch-thumb': {
    backgroundColor: theme.palette.mode === 'light' ? '#fff7ed' : '#1e293b',
  },
  '& .MuiSwitch-thumb:before': {
    content: '"☀️"',
    position: 'absolute',
    inset: 0,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: '13px',
  },
  '& .MuiSwitch-switchBase.Mui-checked .MuiSwitch-thumb': {
    backgroundColor: '#0f172a',
  },
  '& .MuiSwitch-switchBase.Mui-checked .MuiSwitch-thumb:before': {
    content: '"🌙"',
    fontSize: '12px',
  },
}));

/**
 * Styled switch that reflects the deploy lock state across light/dark themes
 * while providing distinct visuals for locked/unlocked/disabled states.
 */
const LockSwitch = styled(BaseSwitch)(({ theme }) => ({
  '& .MuiSwitch-thumb': {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    color: theme.palette.mode === 'light' ? '#047857' : '#dcfce7',
    backgroundColor: theme.palette.mode === 'light' ? '#bbf7d0' : '#0f172a',
    boxShadow:
      theme.palette.mode === 'light'
        ? '0 4px 10px rgba(6, 95, 70, 0.28)'
        : '0 4px 10px rgba(15, 23, 42, 0.35)',
    position: 'relative',
  },
  '& .MuiSwitch-thumb::before': {
    content: '"🔓"',
    position: 'absolute',
    inset: 4,
    borderRadius: '50%',
    backgroundColor: theme.palette.mode === 'light' ? '#dcfce7' : '#134e4a',
    opacity: theme.palette.mode === 'light' ? 0.92 : 0.8,
    zIndex: 0,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: '12px',
    color: theme.palette.mode === 'light' ? '#047857' : '#dcfce7',
  },
  '& .MuiSwitch-thumb::after': {
    content: '""',
    position: 'relative',
    zIndex: 1,
  },
  '& .MuiSwitch-track': {
    background: 'linear-gradient(135deg, #bbf7d0 0%, #22c55e 100%)',
    opacity: 1,
  },
  '& .MuiSwitch-switchBase.Mui-checked + .MuiSwitch-track': {
    background: 'linear-gradient(135deg, #f87171 0%, #be123c 100%)',
  },
  '& .MuiSwitch-switchBase.Mui-checked .MuiSwitch-thumb': {
    backgroundColor: theme.palette.mode === 'light' ? '#fecdd3' : '#7f1d1d',
    color: theme.palette.mode === 'light' ? '#7f1d1d' : '#fde2e2',
    boxShadow: '0 4px 12px rgba(190, 18, 60, 0.32)',
  },
  '& .MuiSwitch-switchBase.Mui-checked .MuiSwitch-thumb::before': {
    content: '"🔒"',
    backgroundColor: theme.palette.mode === 'light' ? '#fee2e2' : '#7f1d1d',
    opacity: theme.palette.mode === 'light' ? 0.95 : 0.85,
    color: theme.palette.mode === 'light' ? '#7f1d1d' : '#fde2e2',
  },
  '& .MuiSwitch-switchBase:not(.Mui-checked) .MuiSwitch-thumb': {
    backgroundColor: theme.palette.mode === 'light' ? '#bbf7d0' : '#0f172a',
  },
  '&.Mui-disabled': {
    opacity: 0.5,
  },
  '&.Mui-disabled .MuiSwitch-track': {
    background: 'linear-gradient(135deg, rgba(148, 163, 184, 0.3) 0%, rgba(148, 163, 184, 0.45) 100%)',
  },
  '&.Mui-disabled .MuiSwitch-switchBase.Mui-checked + .MuiSwitch-track': {
    background: 'linear-gradient(135deg, rgba(148, 163, 184, 0.35) 0%, rgba(148, 163, 184, 0.5) 100%)',
  },
  '&.Mui-disabled .MuiSwitch-thumb': {
    boxShadow: 'none',
    backgroundColor: theme.palette.mode === 'light' ? '#e2e8f0' : '#1e293b',
    color: theme.palette.text.disabled,
  },
  '&.Mui-disabled .MuiSwitch-thumb::before': {
    backgroundColor: theme.palette.mode === 'light' ? '#f1f5f9' : '#0f172a',
    color: theme.palette.text.disabled,
  },
}));

const Sidebar: React.FC<SidebarProps> = ({ open, onClose }) => {
  const [configData, setConfigData] = useState<ConfigData | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const authContext = useContext(AuthContext);
  if (!authContext) {
    throw new Error('AuthContext must be used within an AuthProvider');
  }

  const { authenticated, keycloakToken, groups, privilegedGroups } = authContext;
  const deployLock = useDeployLock();
  const { mode, toggleMode } = useThemeMode();
  const isDarkMode = mode === 'dark';
  const isKeycloakEnabled = Boolean(configData?.keycloak?.enabled);
  const userGroups = groups ?? [];
  const allowedGroups = privilegedGroups ?? [];
  const hasDeployPrivileges = userGroups.some(group => allowedGroups.includes(group));
  // Gray out the control while the configuration is loading or the user lacks deploy privileges.
  const lockSwitchDisabled = isLoading || (isKeycloakEnabled && !hasDeployPrivileges);

  const handleThemeChange = useCallback(
    (_event: React.ChangeEvent<HTMLInputElement>, _checked: boolean) => {
      toggleMode();
    },
    [toggleMode],
  );

  const toggleDeployLock = useCallback(async () => {
    if (lockSwitchDisabled) {
      return;
    }
    try {
      if (deployLock) {
        await releaseDeployLock(authenticated ? keycloakToken : null);
      } else {
        await setDeployLock(authenticated ? keycloakToken : null);
      }
    } catch (error) {
      console.error('Failed to toggle deploy lock:', error);
    }
  }, [deployLock, authenticated, keycloakToken, lockSwitchDisabled]);

  useEffect(() => {
    const loadConfig = async () => {
      try {
        const data = await fetchConfig();
        setConfigData(data);
      } catch (error) {
        if (error instanceof Error) {
          setError(error.message);
        } else {
          setError('An unknown error occurred');
        }
      } finally {
        setIsLoading(false);
      }
    };

    // Handle promise directly within useEffect
    loadConfig();
  }, []);

  const handleCopy = useCallback(() => {
    if (configData) {
      navigator.clipboard.writeText(JSON.stringify(configData, null, 2)).catch(err => {
        console.error('Failed to copy config data to clipboard: ', err);
      });
    }
  }, [configData]);

  const renderTableCell = useCallback((key: string, value: any) => {
    if (key === 'argo_cd_url' && value && typeof value === 'object' && value.constructor === Object) {
      if ('Scheme' in value && 'Host' in value && 'Path' in value) {
        return `${value.Scheme}://${value.Host}${value.Path}`;
      } else {
        return 'Invalid value';
      }
    }

    return (
      <Box sx={{ maxHeight: '100px', overflow: 'auto', whiteSpace: 'nowrap' }}>
        {typeof value === 'object' ? JSON.stringify(value, null, 2) : value.toString()}
      </Box>
    );
  }, []);

  const renderContent = () => {
    if (isLoading) {
      return (
        <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', p: 2 }}>
          <CircularProgress />
          <Typography ml={2}>Loading...</Typography>
        </Box>
      );
    }

    if (error) {
      return <Typography color="error">{error}</Typography>;
    }

    if (configData) {
      return (
        <>
          <TableContainer component={Paper}>
            <Table aria-label="config table">
              <TableHead>
                <TableRow>
                  <TableCell>Key</TableCell>
                  <TableCell>Value</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {Object.entries(configData).map(([key, value]) => (
                  <TableRow key={key} sx={{ '&:nth-of-type(odd)': { backgroundColor: 'action.hover' } }}>
                    <TableCell component="th" scope="row">
                      {key}
                    </TableCell>
                    <TableCell>{renderTableCell(key, value)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
          <Box sx={{ display: 'flex', justifyContent: 'center', marginTop: '20px' }}>
            <Button variant="contained" color="primary" onClick={handleCopy}>
              Copy JSON
            </Button>
          </Box>
        </>
      );
    }

    return <Typography>No data available</Typography>;
  };

  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
      sx={theme => ({
        '& .MuiDrawer-paper': {
          width: '350px',
          backgroundColor: theme.palette.background.paper,
        },
      })}
    >
      <Box
        p={2}
        sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', flex: '1 1 auto' }}
      >
        <Typography variant="h5" gutterBottom>
          Config Data
        </Typography>
        {renderContent()}
      </Box>
      <Box sx={{ px: 2, pb: 1.5, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
        <ControlCard elevation={0} variant="outlined">
          <Box>
            <Typography variant="subtitle1" fontWeight={600}>
              Appearance
            </Typography>
            <Typography variant="body2" color="text.secondary">
              {isDarkMode ? 'Dark mode is active' : 'Light mode is active'}
            </Typography>
          </Box>
          <ThemeSwitch
            checked={isDarkMode}
            onChange={handleThemeChange}
            inputProps={{ 'aria-label': 'Toggle dark mode' }}
          />
        </ControlCard>
        <ControlCard elevation={0} variant="outlined">
          <Box>
            <Typography variant="subtitle1" fontWeight={600}>
              Lockdown Mode
            </Typography>
            <Typography variant="body2" color="text.secondary">
              Pause deployments and enforce read-only state while active.
            </Typography>
          </Box>
          <LockSwitch
            checked={deployLock}
            onChange={toggleDeployLock}
            disabled={lockSwitchDisabled}
            inputProps={{ 'aria-label': 'Toggle deploy lockdown mode' }}
          />
        </ControlCard>
      </Box>
      <Box p={2} sx={theme => ({ borderTop: `1px solid ${theme.palette.divider}` })}>
        <Typography variant="body2" color="text.secondary" align="center">
          © 2022 - {new Date().getFullYear()} Vadim Gedz
        </Typography>
      </Box>
    </Drawer>
  );
};

export default React.memo(Sidebar);
