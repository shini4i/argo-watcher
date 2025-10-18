import React, { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Stack from '@mui/material/Stack';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import IconButton from '@mui/material/IconButton';
import Button from '@mui/material/Button';
import Modal from '@mui/material/Modal';
import RefreshIcon from '@mui/icons-material/Refresh';
import FileDownloadIcon from '@mui/icons-material/FileDownload';
import Checkbox from '@mui/material/Checkbox';
import FormControlLabel from '@mui/material/FormControlLabel';
import DatePicker from 'react-datepicker';
import 'react-datepicker/dist/react-datepicker.css';
import FormGroup from '@mui/material/FormGroup';
import Tooltip from '@mui/material/Tooltip';
import { endOfDay, startOfDay } from 'date-fns';
import { unparse } from 'papaparse';
import * as XLSX from 'xlsx';

import ApplicationsFilter from './ApplicationsFilter';
import TasksTable, { useTasks } from './TasksTable';
import { useErrorContext } from '../ErrorContext';

const modalStyle = {
    position: 'absolute',
    top: '50%',
    left: '50%',
    transform: 'translate(-50%, -50%)',
    width: 300,
    bgcolor: 'background.paper',
    boxShadow: 24,
    p: 4,
    borderRadius: 2,
};

const HistoryTasks: React.FC = () => {
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
    const [isExportModalOpen, setExportModalOpen] = useState(false);
    const [anonymize, setAnonymize] = useState(false);

    /**
     * Synchronizes the visible filters with the URL search params so the current view can
     * be shared or reloaded without losing context.
     *
     * @param start The selected start date or null when no range is defined.
     * @param end The selected end date or null when no range is defined.
     * @param application The selected application identifier.
     * @param page The current pagination page.
     */
    const updateSearchParameters = (
        start: Date | null,
        end: Date | null,
        application: string | null,
        page: number
    ) => {
        const params: Record<string, string> = {};

        if (start && end) {
            params.start = Math.floor(start.getTime() / 1000).toString();
            params.end = Math.floor(end.getTime() / 1000).toString();
        }

        if (application) {
            params.app = application;
        }

        if (page !== 1) {
            params.page = page.toString();
        }

        setSearchParams(params);
    };

    /**
     * Loads data for the provided date range and application filter while keeping the
     * local cache and URL in sync.
     *
     * @param start The start of the selected range.
     * @param end The end of the selected range.
     * @param application The application filter currently applied.
     * @param page The current page in the table pagination.
     */
    const refreshWithFilters = (
        start: Date | null,
        end: Date | null,
        application: string | null,
        page: number
    ) => {
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

    /**
     * Exports the currently loaded tasks into a selected format while applying
     * optional anonymisation to hide sensitive information.
     *
     * @param type The export format (JSON, CSV, or XLSX).
     */
    const exportData = (type: 'json' | 'csv' | 'xlsx') => {
        if (!tasks || tasks.length === 0) {
            setError('export_error', 'No tasks available for export.');
            return;
        }

        let exportTasks = tasks;

        if (anonymize) {
            // Anonymize the data by removing sensitive fields
            exportTasks = tasks.map(({ author, status_reason, ...rest }) => rest);
        }

        if (type !== 'json') {
            exportTasks = exportTasks.map((task) => ({
                ...task,
                created: new Date(task.created * 1000).toLocaleString('en-GB', { hour12: false }),
                updated: task.updated ? new Date(task.updated * 1000).toLocaleString('en-GB', { hour12: false }) : null,
                images: task.images.map((img) => `${img.image}:${img.tag}`).join(', '),
                ...(anonymize
                    ? {}
                    : { status_reason: task.status_reason?.replaceAll('\n', String.raw`\n`) }),
            }));
        }

        const filename = `tasks_export_${Date.now()}.${type}`;
        if (type === 'json') {
            const blob = new Blob([JSON.stringify(exportTasks, null, 2)], { type: 'application/json' });
            const link = document.createElement('a');
            link.href = URL.createObjectURL(blob);
            link.download = filename;
            link.click();
        } else if (type === 'csv') {
            const csv = unparse(exportTasks);
            const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
            const link = document.createElement('a');
            link.href = URL.createObjectURL(blob);
            link.download = filename;
            link.click();
        } else if (type === 'xlsx') {
            const worksheet = XLSX.utils.json_to_sheet(exportTasks);
            const workbook = XLSX.utils.book_new();
            XLSX.utils.book_append_sheet(workbook, worksheet, 'Tasks');
            XLSX.writeFile(workbook, filename);
        }

        setExportModalOpen(false);
        setSuccess('Data exported successfully.');
    };
    useEffect(() => {
        refreshWithFilters(startDate, endDate, currentApplication, currentPage);
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
                        <Button
                            variant="contained"
                            startIcon={<FileDownloadIcon />}
                            onClick={() => setExportModalOpen(true)}
                        >
                            Export
                        </Button>
                    </Box>
                    <Box>
                        <ApplicationsFilter
                            value={currentApplication}
                            onChange={value => {
                                setCurrentApplication(value);
                                setCurrentPage(1);
                                refreshWithFilters(startDate, endDate, value, 1);
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
                                <TextField
                                    size="small"
                                    sx={{ minWidth: '220px' }}
                                    value={`${startDate ? startDate.toLocaleDateString() : ''} - ${endDate ? endDate.toLocaleDateString() : ''}`}
                                    label="Date range"
                                    InputProps={{ readOnly: true }}
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
                            page
                        );
                    }}
                />
            </Box>
            <Modal
                open={isExportModalOpen}
                onClose={() => setExportModalOpen(false)}
                aria-labelledby="export-modal-title"
                aria-describedby="export-modal-description"
            >
                <Box
                    sx={{
                        ...modalStyle,
                        width: 'auto',
                        maxWidth: '90%',
                        textAlign: 'center',
                        whiteSpace: 'nowrap',
                    }}
                >
                    <Typography id="export-modal-title" variant="h6" component="h2" sx={{ mb: 2 }}>
                        Select Export Type
                    </Typography>
                    <Stack direction="row" spacing={2} sx={{ justifyContent: 'center', mb: 2 }}>
                        <Button variant="contained" onClick={() => exportData('json')}>
                            .JSON
                        </Button>
                        <Button variant="contained" onClick={() => exportData('csv')}>
                            .CSV
                        </Button>
                        <Button variant="contained" onClick={() => exportData('xlsx')}>
                            .XLSX
                        </Button>
                    </Stack>
                    <FormGroup sx={{ alignItems: 'center' }}>
                        <Tooltip title="Remove author, and status_reason from exported data">
                            <FormControlLabel
                                control={
                                    <Checkbox
                                        checked={anonymize}
                                        onChange={(e) => setAnonymize(e.target.checked)}
                                    />
                                }
                                label="Anonymize"
                            />
                        </Tooltip>
                    </FormGroup>
                </Box>
            </Modal>
        </Container>
    );
};

export default HistoryTasks;
