import React, { useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Box,
  FormControl,
  IconButton,
  InputLabel,
  MenuItem,
  Select,
  SelectChangeEvent,
  Stack,
  Typography,
} from '@mui/material';
import RefreshIcon from '@mui/icons-material/Refresh';

import ApplicationsFilter from './ApplicationsFilter';
import TasksTable, { useTasks } from './TasksTable';
import { useErrorContext } from '../ErrorContext';

const autoRefreshIntervals: { [key: string]: number } = {
  '5s': 5,
  '10s': 10,
  '30s': 30,
  '1m': 60,
  off: 0,
};

const RecentTasks: React.FC = () => {
  const { setError, setSuccess } = useErrorContext();
  const [searchParams, setSearchParams] = useSearchParams();
  const { tasks, sortField, setSortField, appNames, refreshTasksInTimeframe } = useTasks({
    setError,
    setSuccess,
  });

  const [currentAutoRefresh, setCurrentAutoRefresh] = useState<number>(() =>
    Number(localStorage.getItem('refresh')) || autoRefreshIntervals['30s']
  );

  const autoRefreshIntervalRef = useRef<NodeJS.Timeout | null>(null);
  const [currentApplication, setCurrentApplication] = useState<string | null>(
    searchParams.get('app')
  );
  const currentTimeframe = 9 * 60 * 60;
  const [currentPage, setCurrentPage] = useState<number>(
    searchParams.get('page') ? Number(searchParams.get('page')) : 1
  );

  const updateSearchParameters = (application: string | null, page: number) => {
    const params = new URLSearchParams();
    if (application) {
      params.set('app', application);
    }
    if (page !== 1) {
      params.set('page', page.toString());
    }
    setSearchParams(params);
  };

  useEffect(() => {
    refreshTasksInTimeframe(currentTimeframe, currentApplication);
    const initialPage = searchParams.get('page') ? Number(searchParams.get('page')) : 1;
    updateSearchParameters(currentApplication, initialPage);
  }, []);

  useEffect(() => {
    localStorage.setItem('refresh', currentAutoRefresh.toString());
  }, [currentAutoRefresh]);

  useEffect(() => {
    if (autoRefreshIntervalRef.current !== null) {
      clearInterval(autoRefreshIntervalRef.current);
    }

    if (!currentAutoRefresh) {
      return;
    }

    autoRefreshIntervalRef.current = setInterval(() => {
      refreshTasksInTimeframe(currentTimeframe, currentApplication);
    }, currentAutoRefresh * 1000);

    return () => {
      if (autoRefreshIntervalRef.current !== null) {
        clearInterval(autoRefreshIntervalRef.current);
      }
    };
  }, [currentAutoRefresh, currentApplication, currentTimeframe]);

  const handleAutoRefreshChange = (event: SelectChangeEvent<number>) => {
    const value = Number(event.target.value);
    setCurrentAutoRefresh(value);
    updateSearchParameters(currentApplication, 1);
  };

  return (
    <Box
      sx={{
        px: { xs: 2, md: 3 },
        py: 2,
        display: 'flex',
        flexDirection: 'column',
        gap: 2,
        width: '100%',
        height: '100%',
        boxSizing: 'border-box',
      }}
    >
      <Stack direction={{ xs: 'column', md: 'row' }} spacing={2} alignItems="center">
        <Typography
          variant="h5"
          gutterBottom
          component="div"
          sx={{
            flexGrow: 1,
            display: 'flex',
            gap: '10px',
            m: 0,
            alignItems: 'center',
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
                setCurrentPage(1);
                updateSearchParameters(value, 1);
              }}
              appNames={appNames}
            />
          </Box>
          <Box sx={{ minWidth: 120 }}>
            <FormControl fullWidth size="small">
              <InputLabel>Auto-Refresh</InputLabel>
              <Select
                value={currentAutoRefresh}
                label="Auto-Refresh"
                onChange={handleAutoRefreshChange}
              >
                {Object.keys(autoRefreshIntervals).map(autoRefreshInterval => {
                  const value = autoRefreshIntervals[autoRefreshInterval];
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
              color="primary"
              title="Force table load"
              onClick={() => {
                refreshTasksInTimeframe(currentTimeframe, currentApplication);
                setCurrentPage(1);
                updateSearchParameters(currentApplication, 1);
              }}
            >
              <RefreshIcon />
            </IconButton>
          </Box>
        </Stack>
      </Stack>
      <Box sx={{ flexGrow: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
        <TasksTable
          tasks={tasks}
          sortField={sortField}
          setSortField={setSortField}
          relativeDate={true}
          page={currentPage}
          onPageChange={page => {
            setCurrentPage(page);
            updateSearchParameters(currentApplication, page);
          }}
        />
      </Box>
    </Box>
  );
};

export default RecentTasks;
