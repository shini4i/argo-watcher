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

  // initial load
  useEffect(() => {
    refreshTasksInTimeframe(currentTimeframe, currentApplication);
    updateSearchParameters(currentApplication, currentAutoRefresh, currentPage);
  }, []);

  // we reset interval on any state change (because we use the state variables for data retrieval)
  useEffect(() => {
    // reset current interval
    if (autoRefreshIntervalRef.current !== null) {
      clearInterval(autoRefreshIntervalRef.current);
    }
    if (!currentAutoRefresh) {
      // value is 0 for "off"
      return;
    }
    // set interval
    autoRefreshIntervalRef.current = setInterval(() => {
      refreshTasksInTimeframe(currentTimeframe, currentApplication);
    }, currentAutoRefresh * 1000);

    // clear interval on exit
    return () => {
      if (autoRefreshIntervalRef.current !== null) {
        clearInterval(autoRefreshIntervalRef.current);
      }
    };
  });

  const handleAutoRefreshChange = event => {
    // change value
    setCurrentAutoRefresh(event.target.value);
    // save to URL
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
          variant="h4"
          gutterBottom
          component="div"
          sx={{ flexGrow: 1, display: 'flex', gap: '10px' }}
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
                // reset page
                setCurrentPage(1);
                // update url
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
              title={'force table load'}
              onClick={() => {
                // update tasks
                refreshTasksInTimeframe(currentTimeframe, currentApplication);
                // reset page
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
      <TasksTable
        tasks={tasks}
        sortField={sortField}
        setSortField={setSortField}
        relativeDate={true}
        page={currentPage}
        onPageChange={page => {
          setCurrentPage(page);
          updateSearchParameters(currentApplication, currentAutoRefresh, page);
        }}
      />
    </Container>
  );
}

export default RecentTasks;
