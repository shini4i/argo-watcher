import React, { useEffect, useState } from 'react';
import axios from 'axios';
import Drawer from '@mui/material/Drawer';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

function Sidebar({ open, onClose }) {
    const [configData, setConfigData] = useState(null);

    useEffect(() => {
        if (open) {
            axios.get('/api/v1/config')
                .then(response => {
                    setConfigData(response.data);
                })
                .catch(error => {
                    console.error('Error fetching config data: ', error);
                });
        }
    }, [open]);

    return (
        <Drawer anchor="right" open={open} onClose={onClose}>
            <Box sx={{ width: 250, p: 2 }}>
                <Typography variant="h6">Config Data</Typography>
                {configData && (
                    <pre>{JSON.stringify(configData, null, 2)}</pre>
                )}
            </Box>
        </Drawer>
    );
}

export default Sidebar;
