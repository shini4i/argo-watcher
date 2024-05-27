import React, { forwardRef, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Stack from '@mui/material/Stack';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import IconButton from '@mui/material/IconButton';
import RefreshIcon from '@mui/icons-material/Refresh';
import DatePicker from 'react-datepicker';
import 'react-datepicker/dist/react-datepicker.css';
import { endOfDay, startOfDay } from 'date-fns';

import ApplicationsFilter from './ApplicationsFilter';
import TasksTable, { useTasks } from './TasksTable';
import { useErrorContext } from '../ErrorContext';

interface DateRangePickerCustomInputProps {
  value: string;
  onClick: () => void;
}

const DateRangePickerCustomInput = forwardRef<HTMLInputElement, DateRangePickerCustomInputProps>(
  ({ value, onClick }, ref) => (
    <TextField
      size="small"
      sx={{ minWidth: '220px' }}
      onClick={onClick}
      ref={ref}
      value={value}
      label="Date range"
      InputProps={{ readOnly: true }}
    />
  )
);

DateRangePickerCustomInput.displayName = 'DateRangePickerCustomInput';

interface HistoryTasksProps {}

const HistoryTasks: React.FC<HistoryTasksProps> = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { setError, setSuccess } = useErrorContext();
  const { tasks, sortField, setSortField, appNames, refreshTasksInRange, clearTasks } =
    useTasks({ setError, setSuccess });
  const [currentApplication, setCurrentApplication] = useState<string | null>(
    searchParams.get('app')
  );
  const [dateRange, setDateRange] = useState<[Date | null, Date | null]>([
    searchParams.get('start') ? new Date(Number(searchParams.get('start')) * 1000) : startOfDay(new Date()),
    searchParams.get('end') ? new Date(Number(searchParams.get('end')) * 1000) : startOfDay(new Date()),
  ]);
  const [startDate, endDate] = dateRange;
  const [currentPage, setCurrentPage] = useState<number>(
    searchParams.get('page') ? Number(searchParams.get('page')) : 1
  );

  const updateSearchParameters = (start: Date, end: Date, application: string | null, page: number) => {
    const params: Record<string, any> = {
      start: Math.floor(start.getTime() / 1000),
      end: Math.floor(end.getTime() / 1000),
    };

    if (application) {
      params.app = application;
    }

    if (page !== 1) {
      params.page = page;
    }

    setSearchParams(params);
  };

  const refreshWithFilters = (start: Date | null, end: Date | null, application: string | null, page: number) => {
    if (start && end) {
      refreshTasksInRange(
        Math.floor(startOfDay(start).getTime() / 1000),
        Math.floor(endOfDay(end).getTime() / 1000),
        application
      );
      updateSearchParameters(start, end, application, page);
    } else {
      clearTasks();
    }
  };

  useEffect(() => {
    refreshWithFilters(startDate!, endDate!, currentApplication, currentPage);
  }, [startDate, endDate, currentApplication, currentPage]);

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
            alignItems: 'center',
          }}
        >
          <Box>History tasks</Box>
          <Box sx={{ fontSize: '10px' }}>UTC</Box>
        </Typography>
        <Stack direction="row" spacing={2}>
          <Box>
            <ApplicationsFilter
              value={currentApplication}
              onChange={value => {
                setCurrentApplication(value);
                setCurrentPage(1);
                refreshWithFilters(startDate!, endDate!, value, 1);
              }}
              appNames={appNames}
            />
          </Box>
          <Box>
            <DatePicker
              selectsRange
              startDate={startDate}
              endDate={endDate}
              onChange={(update: [Date | null, Date | null]) => {
                setDateRange(update);
                setCurrentPage(1);
                refreshWithFilters(update[0], update[1], currentApplication, 1);
              }}
              maxDate={new Date()}
              isClearable={false}
              customInput={
                <DateRangePickerCustomInput
                  value={`${startDate ? startDate.toLocaleDateString() : ''} - ${endDate ? endDate.toLocaleDateString() : ''}`}
                  onClick={() => {}}
                />
              }
              required
            />
          </Box>
          <Box>
            <IconButton
              edge="start"
              color="primary"
              title="Reload table"
              onClick={() => {
                setCurrentPage(1);
                refreshWithFilters(startDate!, endDate!, currentApplication, 1);
              }}
            >
              <RefreshIcon />
            </IconButton>
          </Box>
        </Stack>
      </Stack>
      <Box sx={{ boxShadow: 2, borderRadius: 2, p: 2 }}>
        <TasksTable
          tasks={tasks}
          sortField={sortField}
          setSortField={setSortField}
          relativeDate={false}
          page={currentPage}
          onPageChange={page => {
            setCurrentPage(page);
            updateSearchParameters(
              startDate!,
              endDate!,
              currentApplication,
              page
            );
          }}
        />
      </Box>
    </Container>
  );
};

export default HistoryTasks;
