import React, { useContext, useEffect, useState } from 'react';
import PropTypes from 'prop-types';
import {
  Box,
  Button,
  CircularProgress,
  Drawer,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import Switch from '@mui/material/Switch';

import { fetchConfig } from '../Services/Data';
import { releaseDeployLock, setDeployLock, useDeployLock } from '../Services/DeployLockHandler';
import { AuthContext } from '../Services/Auth';

function Sidebar({ open, onClose }) {
  const [configData, setConfigData] = useState(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState(null);

  const { authenticated, keycloakToken } = useContext(AuthContext);
  const deployLock = useDeployLock();

  const toggleDeployLock = async () => {
    if (deployLock) {
      await releaseDeployLock(authenticated ? keycloakToken : null);
    } else {
      await setDeployLock(authenticated ? keycloakToken : null);
    }
  };

  useEffect(() => {
    fetchConfig()
      .then(data => {
        setConfigData(data);
        setIsLoading(false);
      })
      .catch(error => {
        setError(error.message);
        setIsLoading(false);
      });
  }, []);

  const handleCopy = () => {
    navigator.clipboard.writeText(JSON.stringify(configData, null, 2)).catch(err => {
        console.error('Failed to copy config data to clipboard: ', err);
      });
  };

  const renderTableCell = (key, value) => {
    if (key === 'argo_cd_url' && value && typeof value === 'object' && value.constructor === Object) {
      if ('Scheme' in value && 'Host' in value && 'Path' in value) {
        return `${value.Scheme}://${value.Host}${value.Path}`;
      } else {
        return 'Invalid value';
      }
    } else if (value && typeof value === 'object' && value.constructor === Object) {
      return (
        <Box sx={{ maxHeight: '100px', overflow: 'auto', whiteSpace: 'nowrap' }}>
          {JSON.stringify(value, null, 2)}
        </Box>
      );
    }
    return (
      <Box sx={{ maxHeight: '100px', overflow: 'auto', whiteSpace: 'nowrap' }}>
        {value.toString()}
      </Box>
    );
  };

  const renderContent = () => {
    if (isLoading) {
      return (
        <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', p: 2 }}>
          <CircularProgress />
          <Typography ml={2}>Loading...</Typography>
        </Box>
      );
    } else if (error) {
      return <Typography color="error">{error}</Typography>;
    } else if (configData) {
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
                    <TableCell>
                      {renderTableCell(key, value)}
                    </TableCell>
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
    } else {
      return <Typography>No data available</Typography>;
    }
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
          <Typography variant="body1">
            Lockdown Mode
          </Typography>
          <Switch
            checked={deployLock}
            onChange={toggleDeployLock}
            color="primary"
          />
        </Box>
      </Paper>
      <Box p={2} sx={{ borderTop: '1px solid gray' }}>
        <Typography variant="body2" color="textSecondary" align="center">
          © 2022 - {new Date().getFullYear()} Vadim Gedz
        </Typography>
      </Box>
    </Drawer>
  );
}

Sidebar.propTypes = {
  open: PropTypes.bool.isRequired,
  onClose: PropTypes.func.isRequired,
};

export default Sidebar;
