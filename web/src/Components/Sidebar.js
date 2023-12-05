import React, { useEffect, useState } from 'react';
import PropTypes from 'prop-types';
import axios from 'axios';
import {
    Drawer, Box, Typography, TableContainer, Table, TableHead, TableRow, TableCell,
    TableBody, Paper, CircularProgress, Button
} from '@mui/material';

function Sidebar({ open, onClose }) {
    const [configData, setConfigData] = useState(null);
    const [isLoading, setIsLoading] = useState(false);
    const [error, setError] = useState(null);

    useEffect(() => {
        if (open) {
            setIsLoading(true);
            axios.get('/api/v1/config')
                .then(response => {
                    setConfigData(response.data);
                    setError(null);
                })
                .catch(error => {
                    console.error('Error fetching config data: ', error);
                    setError('Failed to fetch config data');
                })
                .finally(() => {
                    setIsLoading(false);
                });
        }
    }, [open]);

    const handleCopy = () => {
        navigator.clipboard.writeText(JSON.stringify(configData, null, 2));
    };

    const renderTableCell = (key, value) => {
        if (key === 'argo_cd_url' && value && typeof value === 'object' && value.constructor === Object) {
            return `${value.Scheme}://${value.Host}${value.Path}`;
        } else if (value && typeof value === 'object' && value.constructor === Object) {
            return JSON.stringify(value, null, 2);
        }
        return value.toString();
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
                                    <TableCell>Config Key</TableCell>
                                    <TableCell>Config Value</TableCell>
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
            <Box p={2}>
                <Typography variant="h6" gutterBottom>
                    Config Data
                </Typography>
                {renderContent()}
            </Box>
        </Drawer>
    );
}

Sidebar.propTypes = {
    open: PropTypes.bool.isRequired,
    onClose: PropTypes.func.isRequired
};

export default Sidebar;