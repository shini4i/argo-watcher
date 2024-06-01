import React, { useEffect, useState } from 'react';
import { Link as ReactLink } from 'react-router-dom';
import { addMinutes, format } from 'date-fns';
import {
  Box,
  MenuItem,
  Pagination,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
  Link,
} from '@mui/material';
import CheckCircleOutlineIcon from '@mui/icons-material/CheckCircleOutline';
import CancelOutlinedIcon from '@mui/icons-material/CancelOutlined';
import CircularProgress from '@mui/material/CircularProgress';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';
import KeyboardArrowDownIcon from '@mui/icons-material/KeyboardArrowDown';
import KeyboardArrowUpIcon from '@mui/icons-material/KeyboardArrowUp';
import LaunchIcon from '@mui/icons-material/Launch';

import { fetchTasks } from '../Services/Data';
import { useDeployLock } from '../Services/DeployLockHandler';
import { relativeHumanDuration, relativeTime, relativeTimestamp } from '../Utils';

interface ProjectDisplayProps {
  project: string;
}

export function ProjectDisplay({ project }: ProjectDisplayProps) {
  if (project.startsWith('http')) {
    return (
      <Link href={project}>
        {project.replace(/^http(s)?:\/\//, '').replace(/\/+$/, '')}
      </Link>
    );
  }
  return <Typography variant={'body2'}>{project}</Typography>;
}

interface StatusReasonDisplayProps {
  reason: string;
}

export function StatusReasonDisplay({ reason }: StatusReasonDisplayProps) {
  return (
    <Typography
      sx={{
        width: '100%',
        overflow: 'auto',
        fontSize: '13px',
      }}
      component={'pre'}
    >
      {reason}
    </Typography>
  );
}

const taskDuration = (created: number, updated: number | null): string => {
  if (!updated) {
    updated = Math.round(Date.now() / 1000);
  }
  const seconds = updated - created;
  return relativeHumanDuration(seconds);
};

const defaultFormatTime = '---';
export const formatDateTime = (timestamp: number | null): string => {
  if (!timestamp) {
    return defaultFormatTime;
  }
  try {
    let dateTime = new Date(timestamp * 1000);
    return format(
      addMinutes(dateTime, dateTime.getTimezoneOffset()),
      'yyyy/MM/dd HH:mm:ss'
    );
  } catch (error) {
    console.error(error);
    return defaultFormatTime;
  }
};

interface Task {
  id: string;
  app: string;
  project: string;
  author: string;
  status: string;
  created: number;
  updated: number;
  images: { id: string; image: string; tag: string }[];
  status_reason?: string;
}

interface SortField {
  field: keyof Task;
  direction: 'ASC' | 'DESC';
}

interface UseTasksParams {
  setError: (context: string, message: string) => void;
  setSuccess: (context: string, message: string) => void;
}

export function useTasks({ setError, setSuccess }: UseTasksParams) {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [sortField, setSortField] = useState<SortField>({
    field: 'created',
    direction: 'ASC',
  });

  const [appNames, setAppNames] = useState<string[]>([]);

  const refreshTasksInTimeframe = (timeframe: number, application: string | null) => {
    fetchTasks(relativeTimestamp(timeframe), null, application)
      .then(items => {
        setSuccess('fetchTasks', 'Fetched tasks successfully');

        const appNames = Array.from(new Set(items.map((item: Task) => item.app)));
        setAppNames(appNames);

        setTasksSorted(items, sortField);
      })
      .catch(error => {
        setError('fetchTasks', error.message);
      });
  };

  const refreshTasksInRange = (fromTimestamp: number, toTimestamp: number, application: string | null) => {
    fetchTasks(fromTimestamp, toTimestamp, application)
      .then(items => {
        setSuccess('fetchTasks', 'Fetched tasks successfully');

        const appNames = Array.from(new Set(items.map((item: Task) => item.app)));
        setAppNames(appNames);

        setTasksSorted(items, sortField);
      })
      .catch(error => {
        setError('fetchTasks', error.message);
      });
  };

  const clearTasks = () => {
    setTasks([]);
  };

  const setTasksSorted = (unsortedTasks: Task[], sort: SortField) => {
    unsortedTasks.sort((a, b) => {
      const aField = a[sort.field];
      const bField = b[sort.field];
      if (aField === bField) {
        return 0;
      }
      if (aField === undefined || bField === undefined) {
        return 0;
      }
      if (aField > bField) {
        return sort.direction === 'ASC' ? -1 : 1;
      } else {
        return sort.direction === 'ASC' ? 1 : -1;
      }
    });

    setTasks([...unsortedTasks]);
  };

  useEffect(() => {
    setTasksSorted(tasks, sortField);
  }, [sortField]);

  return {
    tasks,
    sortField,
    setSortField,
    refreshTasksInTimeframe,
    refreshTasksInRange,
    clearTasks,
    appNames,
  };
}

interface TableCellSortedProps {
  field: keyof Task;
  sortField: SortField;
  setSortField: (sortField: SortField) => void;
  children: React.ReactNode;
}

function TableCellSorted({ field, sortField, setSortField, children }: TableCellSortedProps) {
  const triggerSortChange = (triggerField: keyof Task) => {
    let sortFieldChange = { ...sortField };
    if (sortFieldChange.field === triggerField) {
      sortFieldChange.direction =
        sortFieldChange.direction === 'ASC' ? 'DESC' : 'ASC';
    } else {
      sortFieldChange.field = triggerField;
      sortFieldChange.direction = 'ASC';
    }
    setSortField(sortFieldChange);
  };

  return (
    <TableCell
      onClick={() => {
        triggerSortChange(field);
      }}
      sx={{ cursor: 'pointer' }}
    >
      <Box sx={{ display: 'flex', flexDirection: 'row' }}>
        {children}{' '}
        {sortField.field === field &&
          (sortField.direction === 'ASC' ? (
            <KeyboardArrowUpIcon />
          ) : (
            <KeyboardArrowDownIcon />
          ))}
      </Box>
    </TableCell>
  );
}

const cacheKeyItemsPerPage = 'items_per_page';
const itemsPerPageList = [10, 25, 50];
const defaultItemsPerPage = itemsPerPageList[0];

const getCachedItemsPerPage = (): number => {
  const itemsPerPage = Number(localStorage.getItem(cacheKeyItemsPerPage));
  if (itemsPerPageList.includes(itemsPerPage)) {
    return itemsPerPage;
  }
  return defaultItemsPerPage;
};

interface TasksTableProps {
  tasks: Task[];
  sortField: SortField;
  setSortField: (sortField: SortField) => void;
  relativeDate: boolean;
  onPageChange: (page: number) => void;
  page?: number;
}

function TasksTable({
                      tasks,
                      sortField,
                      setSortField,
                      relativeDate,
                      onPageChange,
                      page = 1,
                    }: TasksTableProps) {
  const [itemsPerPage, setItemsPerPage] = useState<number>(getCachedItemsPerPage());
  const [visibleReasons, setVisibleReasons] = useState<string[]>([]);
  const deployLock = useDeployLock();

  const toggleReason = (task: Task) => {
    setVisibleReasons((visibleReasons) => {
      if (visibleReasons.includes(task.id)) {
        return visibleReasons.filter((id) => id !== task.id);
      } else {
        return [...visibleReasons, task.id];
      }
    });
  };

  const pages = Math.ceil(tasks.length / itemsPerPage);
  const tasksPaginated = tasks.slice(
    (page - 1) * itemsPerPage,
    page * itemsPerPage
  );

  const handleItemsPerPageChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const value = Number(event.target.value);
    setItemsPerPage(value);
    localStorage.setItem(cacheKeyItemsPerPage, value.toString());
  };

  return (
    <>
      <TableContainer>
        <Table sx={{ minWidth: 650 }}>
          <TableHead>
            <TableRow>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={'id'}>
                Id
              </TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={'app'}>
                Application
              </TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={'project'}>
                Project
              </TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={'author'}>
                Author
              </TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={'status'}>
                Status
              </TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={'created'}>
                Started
              </TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={'updated'}>
                Duration
              </TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={'images'}>
                Images
              </TableCellSorted>
            </TableRow>
          </TableHead>
          <TableBody>
            {tasksPaginated.map((task) => (
              <React.Fragment key={task.id}>
                <TableRow>
                  <TableCell>
                    <Typography
                      to={`/task/${task.id}`}
                      sx={{
                        textDecoration: 'none',
                        color: 'neutral.main',
                        '&:hover': {
                          textDecoration: 'underline',
                        },
                        display: 'flex',
                      }}
                      component={ReactLink}
                      variant={'body2'}
                    >
                      <span>{task.id.substring(0, 8)}</span>
                      <LaunchIcon fontSize="small" sx={{ marginLeft: '5px' }} />
                    </Typography>
                  </TableCell>
                  <TableCell>{task.app}</TableCell>
                  <TableCell>
                    <ProjectDisplay project={task.project} />
                  </TableCell>
                  <TableCell>{task.author}</TableCell>
                  <TableCell>
                    {task.status === 'deployed' && (
                      <Tooltip title="Deployed">
                        <CheckCircleOutlineIcon style={{ color: 'green' }} />
                      </Tooltip>
                    )}
                    {task.status === 'failed' && (
                      <Tooltip title="Failed">
                        <CancelOutlinedIcon
                          style={{ color: 'red', cursor: 'pointer' }}
                          onClick={() => {
                            if (task.status_reason) {
                              toggleReason(task);
                            }
                          }}
                        />
                      </Tooltip>
                    )}
                    {task.status === 'in progress' && (
                      <Tooltip title="In Progress">
                        <CircularProgress size={24} />
                      </Tooltip>
                    )}
                    {task.status === 'app not found' && (
                      <Tooltip title="App Not Found">
                        <ErrorOutlineIcon style={{ color: 'gray' }} />
                      </Tooltip>
                    )}
                  </TableCell>
                  <TableCell>
                    {relativeDate ? (
                      <Tooltip title={formatDateTime(task.created)}>
                        <span>{relativeTime(task.created * 1000)}</span>
                      </Tooltip>
                    ) : (
                      <span>{formatDateTime(task.created)}</span>
                    )}
                  </TableCell>
                  <TableCell>
                    {task.status === 'in progress' ? (
                      <span>{taskDuration(task.created, null)}</span>
                    ) : (
                      <span>{taskDuration(task.created, task.updated)}</span>
                    )}
                  </TableCell>
                  <TableCell>
                    {task.images.map((item) => (
                      <div key={item.id}>
                        {item.image}:{item.tag}
                      </div>
                    ))}
                  </TableCell>
                </TableRow>
                {task.status_reason && visibleReasons.includes(task.id) && (
                  <TableRow
                    sx={{
                      backgroundColor: 'reason_color.main',
                    }}
                  >
                    <TableCell colSpan={8}>
                      <StatusReasonDisplay reason={task.status_reason} />
                    </TableCell>
                  </TableRow>
                )}
              </React.Fragment>
            ))}
            {tasks.length === 0 && (
              <TableRow>
                <TableCell colSpan={100} sx={{ textAlign: 'center' }}>
                  No tasks were found within provided time frame
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </TableContainer>
      <Box
        sx={{
          my: 3,
          display: 'flex',
          justifyContent: 'space-between',
        }}
      >
        <Pagination
          count={pages}
          variant="outlined"
          shape="rounded"
          page={page}
          onChange={(_event, value) => {
            onPageChange(value);
          }}
        />
        <TextField
          select
          sx={{ width: '100px' }}
          label="Items on page"
          value={itemsPerPage}
          onChange={handleItemsPerPageChange}
          size="small"
        >
          {itemsPerPageList.map((value) => (
            <MenuItem key={value} value={value}>
              {value}
            </MenuItem>
          ))}
        </TextField>
      </Box>
      {deployLock && (
        <Box
          sx={{
            position: 'fixed',
            bottom: 0,
            left: 0,
            width: '100%',
            backgroundColor: 'error.main',
            color: 'white',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            py: 2,
          }}
        >
          <Typography variant="h6">Lockdown is active</Typography>
        </Box>
      )}
    </>
  );
}

export default TasksTable;
