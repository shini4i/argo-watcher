import React, { useCallback, useEffect, useState } from 'react';
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
import { fetchConfig } from '../Services/Data';
import { releaseDeployLock, setDeployLock, useDeployLock } from '../Services/DeployLockHandler';
import { useAuth } from '../Services/Auth';

interface ConfigData {
  [key: string]: any;
}

interface SidebarProps {
  open: boolean;
  onClose: () => void;
}

/**
 * Sidebar component that displays configuration data and provides functionality to toggle deploy lock.
 *
 * @component
 * @param {Object} props - The props for the Sidebar component.
 * @param {boolean} props.open - Indicates whether the sidebar is open or closed.
 * @param {Function} props.onClose - The callback function to handle closing the sidebar.
 * @returns {JSX.Element} The rendered Sidebar component.
 */
const Sidebar: React.FC<SidebarProps> = ({ open, onClose }) => {
  const [configData, setConfigData] = useState<ConfigData | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const { authenticated, keycloakToken } = useAuth();
  const deployLock = useDeployLock();

  const toggleDeployLock = useCallback(async () => {
    try {
      if (deployLock) {
        await releaseDeployLock(authenticated ? keycloakToken : null);
      } else {
        await setDeployLock(authenticated ? keycloakToken : null);
      }
    } catch (error) {
      console.error('Failed to toggle deploy lock:', error);
    }
  }, [deployLock, authenticated, keycloakToken]);

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
    <Drawer anchor="right" open={open} onClose={onClose} sx={{ '& .MuiDrawer-paper': { width: '350px' } }}>
      <Box p={2} sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', flex: '1 1 auto' }}>
        <Typography variant="h5" gutterBottom>
          Config Data
        </Typography>
        {renderContent()}
      </Box>
      <Paper elevation={3} sx={{ margin: 2, padding: 2 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <Typography variant="body1" gutterBottom>
            Lockdown Mode
          </Typography>
          <Switch checked={deployLock} onChange={toggleDeployLock} color="primary" />
        </Box>
      </Paper>
      <Box p={2} sx={{ borderTop: '1px solid gray' }}>
        <Typography variant="body2" color="textSecondary" align="center">
          © 2022 - {new Date().getFullYear()} Vadim Gedz
        </Typography>
      </Box>
    </Drawer>
  );
};

export default React.memo(Sidebar);
