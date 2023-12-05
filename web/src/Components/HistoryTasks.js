import React, { forwardRef, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Stack from '@mui/material/Stack';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import IconButton from '@mui/material/IconButton';
import RefreshIcon from '@mui/icons-material/Refresh';
import ApplicationsFilter from './ApplicationsFilter';
import TasksTable, { useTasks } from './TasksTable';
import { endOfDay, startOfDay } from 'date-fns';
import DatePicker from 'react-datepicker';
import 'react-datepicker/dist/react-datepicker.css';
import { useErrorContext } from '../ErrorContext';

function HistoryTasks() {
    const [searchParams, setSearchParams] = useSearchParams();
    const { setError, setSuccess } = useErrorContext();
    const { tasks, sortField, setSortField, refreshTasksInRange, clearTasks } =
        useTasks({ setError, setSuccess });
    const [currentApplication, setCurrentApplication] = useState(
        searchParams.get('app') ?? null,
    );
    const [dateRange, setDateRange] = useState([
        Number(searchParams.get('start'))
            ? new Date(Number(searchParams.get('start')) * 1000)
            : startOfDay(new Date()),
        Number(searchParams.get('end'))
            ? new Date(Number(searchParams.get('end')) * 1000)
            : startOfDay(new Date()),
    ]);
    const [startDate, endDate] = dateRange;
    const [currentPage, setCurrentPage] = useState(
        searchParams.get('page') ? Number(searchParams.get('page')) : 1,
    );

    const updateSearchParameters = (start, end, application, page) => {
        setSearchParams({
            app: application ?? '',
            start: Math.floor(start.getTime() / 1000),
            end: Math.floor(end.getTime() / 1000),
            page,
        });
    };

    const refreshWithFilters = (start, end, application, page) => {
        if (start && end) {
            refreshTasksInRange(
                Math.floor(startOfDay(start).getTime() / 1000),
                Math.floor(endOfDay(end).getTime() / 1000),
                application,
            );
            updateSearchParameters(start, end, application, page);
        } else {
            clearTasks();
        }
    };

    useEffect(() => {
        refreshWithFilters(startDate, endDate, currentApplication, currentPage);
    }, []);

    const DateRangePickerCustomInput = forwardRef(({ value, onClick }, ref) => (
        <TextField
            size="small"
            sx={{ minWidth: '220px' }}
            onClick={onClick}
            ref={ref}
            value={value}
            label={'Date range'}
        />
    ));

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
                                refreshWithFilters(startDate, endDate, value, 1);
                            }}
                            setError={setError}
                            setSuccess={setSuccess}
                        />
                    </Box>
                    <Box>
                        <DatePicker
                            selectsRange={true}
                            startDate={startDate}
                            endDate={endDate}
                            onChange={update => {
                                setDateRange(update);
                                setCurrentPage(1);
                                refreshWithFilters(update[0], update[1], currentApplication, 1);
                            }}
                            maxDate={new Date()}
                            isClearable={false}
                            customInput={<DateRangePickerCustomInput />}
                            required
                        />
                    </Box>
                    <Box>
                        <IconButton
                            edge="start"
                            color={'primary'}
                            title={'Reload table'}
                            onClick={() => {
                                setCurrentPage(1);
                                refreshWithFilters(startDate, endDate, currentApplication, 1);
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
                            startDate,
                            endDate,
                            currentApplication,
                            page,
                        );
                    }}
                />
            </Box>
        </Container>
    );
}

export default HistoryTasks;
