import React, { useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Stack from '@mui/material/Stack';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import RefreshIcon from '@mui/icons-material/Refresh';
import FormControl from '@mui/material/FormControl';
import InputLabel from '@mui/material/InputLabel';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import ApplicationsFilter from './ApplicationsFilter';
import TasksTable, { useTasks } from './TasksTable';
import { useErrorContext } from '../ErrorContext';

const autoRefreshIntervals = {
    '5s': 5,
    '10s': 10,
    '30s': 30,
    '1m': 60,
    off: 0,
};

function RecentTasks() {
    const { setError, setSuccess } = useErrorContext();
    const [searchParams, setSearchParams] = useSearchParams();
    const { tasks, sortField, setSortField, refreshTasksInTimeframe } = useTasks({
        setError,
        setSuccess,
    });

    const [currentAutoRefresh, setCurrentAutoRefresh] = useState(
        searchParams.get('refresh') ?? autoRefreshIntervals['30s'],
    );
    const autoRefreshIntervalRef = useRef(null);
    const [currentApplication, setCurrentApplication] = useState(
        searchParams.get('app') ?? null,
    );
    const currentTimeframe = 9 * 60 * 60;
    const [currentPage, setCurrentPage] = useState(
        searchParams.get('page') ? Number(searchParams.get('page')) : 1,
    );

    const updateSearchParameters = (application, refresh, page) => {
        setSearchParams({
            app: application ?? '',
            refresh,
            page,
        });
    };

    // Initial load
    useEffect(() => {
        refreshTasksInTimeframe(currentTimeframe, currentApplication);
        updateSearchParameters(currentApplication, currentAutoRefresh, currentPage);
    }, []);

    // Reset the interval on any state change (because we use the state variables for data retrieval)
    useEffect(() => {
        // Reset the current interval
        if (autoRefreshIntervalRef.current !== null) {
            clearInterval(autoRefreshIntervalRef.current);
        }

        if (!currentAutoRefresh) {
            // Value is 0 for "off"
            return;
        }

        // Set interval
        autoRefreshIntervalRef.current = setInterval(() => {
            refreshTasksInTimeframe(currentTimeframe, currentApplication);
        }, currentAutoRefresh * 1000);

        // Clear interval on exit
        return () => {
            if (autoRefreshIntervalRef.current !== null) {
                clearInterval(autoRefreshIntervalRef.current);
            }
        };
    });

    const handleAutoRefreshChange = event => {
        // Change the value
        setCurrentAutoRefresh(event.target.value);
        // Save to URL
        updateSearchParameters(currentApplication, event.target.value, 1);
    };

    return (
        <Container maxWidth="xl">
            <Stack
                direction={{ xs: 'column', md: 'row' }}
                spacing={2}
                alignItems="center"
                sx={{ mb: 2 }}
            >
                <Typography
                    variant="h5"
                    gutterBottom
                    component="div"
                    sx={{
                        flexGrow: 1,
                        display: 'flex',
                        gap: '10px',
                        m: 0,
                        alignItems: 'center', // Center text vertically
                    }}
                >
                    <Box>Recent tasks</Box>
                    <Box sx={{ fontSize: '10px' }}>UTC</Box>
                </Typography>
                <Stack direction="row" spacing={2}>
                    <Box>
                        <ApplicationsFilter
                            value={currentApplication}
                            onChange={value => {
                                setCurrentApplication(value);
                                refreshTasksInTimeframe(currentTimeframe, value);
                                // Reset page
                                setCurrentPage(1);
                                // Update URL
                                updateSearchParameters(value, currentAutoRefresh, 1);
                            }}
                            setError={setError}
                            setSuccess={setSuccess}
                        />
                    </Box>
                    <Box sx={{ minWidth: 120 }}>
                        <FormControl fullWidth size={'small'}>
                            <InputLabel>Auto-Refresh</InputLabel>
                            <Select
                                value={currentAutoRefresh}
                                label="Auto-Refresh"
                                onChange={handleAutoRefreshChange}
                            >
                                {Object.keys(autoRefreshIntervals).map(autoRefreshInterval => {
                                    let value = autoRefreshIntervals[autoRefreshInterval];
                                    return (
                                        <MenuItem key={autoRefreshInterval} value={value}>
                                            {autoRefreshInterval}
                                        </MenuItem>
                                    );
                                })}
                            </Select>
                        </FormControl>
                    </Box>
                    <Box>
                        <IconButton
                            edge="start"
                            color={'primary'}
                            title={'Force table load'}
                            onClick={() => {
                                // Update tasks
                                refreshTasksInTimeframe(currentTimeframe, currentApplication);
                                // Reset page
                                setCurrentPage(1);
                                updateSearchParameters(
                                    currentApplication,
                                    currentAutoRefresh,
                                    1,
                                );
                            }}
                        >
                            <RefreshIcon />
                        </IconButton>
                    </Box>
                </Stack>
            </Stack>
            {/* Style the table with Material-UI Paper component */}
            <Box sx={{ boxShadow: 2, borderRadius: 2, p: 2 }}>
                <TasksTable
                    tasks={tasks}
                    sortField={sortField}
                    setSortField={setSortField}
                    relativeDate={true}
                    page={currentPage}
                    onPageChange={page => {
                        setCurrentPage(page);
                        updateSearchParameters(
                            currentApplication,
                            currentAutoRefresh,
                            page,
                        );
                    }}
                />
            </Box>
        </Container>
    );
}

export default RecentTasks;
