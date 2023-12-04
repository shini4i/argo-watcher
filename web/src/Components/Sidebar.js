import React, { useEffect, useState } from 'react';
import axios from 'axios';
import {
    Drawer,
    Box,
    Typography,
    TableContainer,
    Table,
    TableHead,
    TableRow,
    TableCell,
    TableBody,
    Paper,
    CircularProgress
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

    const renderTableCell = (value) => {
        if (value && typeof value === 'object' && value.constructor === Object) {
            return JSON.stringify(value, null, 2);
        }
        return value.toString();
    };

    return (
        <Drawer anchor="right" open={open} onClose={onClose}>
            <Box sx={{ width: 350, p: 2 }}>
                <Typography variant="h6" gutterBottom>
                    Config Data
                </Typography>
                {isLoading ? (
                    <Box sx={{ display: 'flex', justifyContent: 'center', p: 2 }}>
                        <CircularProgress />
                    </Box>
                ) : error ? (
                    <Typography color="error">{error}</Typography>
                ) : configData ? (
                    <TableContainer component={Paper}>
                        <Table sx={{ minWidth: 300 }} aria-label="config table">
                            <TableHead>
                                <TableRow>
                                    <TableCell>Config Key</TableCell>
                                    <TableCell>Config Value</TableCell>
                                </TableRow>
                            </TableHead>
                            <TableBody>
                                {Object.entries(configData).map(([key, value]) => (
                                    <TableRow key={key}>
                                        <TableCell component="th" scope="row">
                                            {key}
                                        </TableCell>
                                        <TableCell>
                                            {renderTableCell(value)}
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    </TableContainer>
                ) : (
                    <Typography>No data available</Typography>
                )}
                <Typography variant="body2" color="text.secondary" align="center" style={{ marginTop: '20px' }}>
                    © {new Date().getFullYear()} Vadim Gedz. This project is licensed under the MIT License.
                </Typography>
            </Box>
        </Drawer>
    );
}

export default Sidebar;
