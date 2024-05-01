import KeyboardArrowDownIcon from '@mui/icons-material/KeyboardArrowDown';
import KeyboardArrowUpIcon from '@mui/icons-material/KeyboardArrowUp';
import LaunchIcon from '@mui/icons-material/Launch';
import { Link, MenuItem, TextField } from '@mui/material';
import Box from '@mui/material/Box';
import Pagination from '@mui/material/Pagination';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import { addMinutes, format } from 'date-fns';
import React, { useEffect, useState } from 'react';
import { Link as ReactLink } from 'react-router-dom';
import { fetchTasks } from '../Services/Data';
import { useDeployLock } from '../deployLockHandler';
import { relativeHumanDuration, relativeTime, relativeTimestamp } from '../Utils';
import CheckCircleOutlineIcon from '@mui/icons-material/CheckCircleOutline';
import CancelOutlinedIcon from '@mui/icons-material/CancelOutlined';
import CircularProgress from '@mui/material/CircularProgress';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';

export function ProjectDisplay({ project }) {
  if (project.indexOf('http') === 0) {
    return (
      <Link href={project}>
        {project.replace(/^http(s)?:\/\//, '').replace(/\/+$/, '')}
      </Link>
    );
  }
  return <Typography variant={'body2'}>{project}</Typography>;
}

export function StatusReasonDisplay({ reason }) {
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

const taskDuration = (created, updated) => {
  if (!updated) {
    updated = Math.round(Date.now() / 1000);
  }
  const seconds = updated - created;
  return relativeHumanDuration(seconds);
};

const defaultFormatTime = '---';
export const formatDateTime = timestamp => {
  if (!timestamp) {
    return defaultFormatTime;
  }
  try {
    let dateTime = new Date(timestamp * 1000);
    return format(
      addMinutes(dateTime, dateTime.getTimezoneOffset()),
      'yyyy/MM/dd HH:mm:ss',
    );
  } catch (error) {
    console.error(error);
    return defaultFormatTime;
  }
};

export function useTasks({ setError, setSuccess }) {
  const [tasks, setTasks] = useState([]);
  const [sortField, setSortField] = useState({
    field: 'created',
    direction: 'ASC',
  });

  const refreshTasksInTimeframe = (timeframe, application) => {
    // get tasks by timestamp
    fetchTasks(relativeTimestamp(timeframe), null, application)
      .then(items => {
        setSuccess('fetchTasks', 'Fetched tasks successfully');
        setTasksSorted(items, sortField);
      })
      .catch(error => {
        setError('fetchTasks', error.message);
      });
  };

  const refreshTasksInRange = (fromTimestamp, toTimestamp, application) => {
    // get tasks by timestamp
    fetchTasks(fromTimestamp, toTimestamp, application)
      .then(items => {
        setSuccess('fetchTasks', 'Fetched tasks successfully');
        setTasksSorted(items, sortField);
      })
      .catch(error => {
        setError('fetchTasks', error.message);
      });
  };

  const clearTasks = () => {
    setTasks([]);
  };

  const setTasksSorted = (unsortedTasks, sort) => {
    // sort tasks
    unsortedTasks.sort((a, b) => {
      let aField = a[sort.field];
      let bField = b[sort.field];
      if (aField === bField) {
        return 0;
      }
      if (aField > bField) {
        return sort.direction === 'ASC' ? -1 : 1;
      } else {
        return sort.direction === 'ASC' ? 1 : -1;
      }
    });

    // save sorted tasks
    setTasks([].concat(unsortedTasks));
  };

  // sort field change hook
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
  };
}

function TableCellSorted({ field, sortField, setSortField, children }) {
  const triggerSortChange = triggerField => {
    // change sort parameters
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
const getCachedItemsPerPage = () => {
  const itemsPerPage = Number(localStorage.getItem(cacheKeyItemsPerPage));
  if (itemsPerPageList.includes(itemsPerPage)) {
    return itemsPerPage;
  }
  return defaultItemsPerPage;
};

function TasksTable({
                      tasks,
                      sortField,
                      setSortField,
                      relativeDate,
                      onPageChange,
                      page = 1,
                    }) {
  const [itemsPerPage, setItemsPerPage] = useState(getCachedItemsPerPage());
  const [visibleReasons, setVisibleReasons] = useState([]);
  const deployLock = useDeployLock();

  const toggleReason = task => {
    setVisibleReasons(visibleReasons => {
      if (visibleReasons.includes(task?.id)) {
        return [...visibleReasons.filter(id => id !== task?.id)];
      } else {
        return [...visibleReasons, task?.id];
      }
    });
  };

  const pages = Math.ceil(tasks.length / itemsPerPage);
  const tasksPaginated = tasks.slice(
    (page - 1) * itemsPerPage,
    page * itemsPerPage,
  );

  const handleItemsPerPageChange = event => {
    setItemsPerPage(event.target.value);
    localStorage.setItem(cacheKeyItemsPerPage, event.target.value);
  };

  return (
    <>
      <TableContainer>
        <Table sx={{ minWidth: 650 }}>
          <TableHead>
            <TableRow>
              <TableCellSorted
                sortField={sortField}
                setSortField={setSortField}
                field={'id'}
              >
                Id
              </TableCellSorted>
              <TableCellSorted
                sortField={sortField}
                setSortField={setSortField}
                field={'app'}
              >
                Application
              </TableCellSorted>
              <TableCellSorted
                sortField={sortField}
                setSortField={setSortField}
                field={'project'}
              >
                Project
              </TableCellSorted>
              <TableCellSorted
                sortField={sortField}
                setSortField={setSortField}
                field={'author'}
              >
                Author
              </TableCellSorted>
              <TableCellSorted
                sortField={sortField}
                setSortField={setSortField}
                field={'status'}
              >
                Status
              </TableCellSorted>
              <TableCellSorted
                sortField={sortField}
                setSortField={setSortField}
                field={'created'}
              >
                Started
              </TableCellSorted>
              <TableCellSorted
                sortField={sortField}
                setSortField={setSortField}
                field={'updated'}
              >
                Duration
              </TableCellSorted>
              <TableCellSorted
                sortField={sortField}
                setSortField={setSortField}
                field={'images'}
              >
                Images
              </TableCellSorted>
            </TableRow>
          </TableHead>
          <TableBody>
            {tasksPaginated.map(task => (
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
                        <CancelOutlinedIcon style={{ color: 'red' }} />
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
                    {relativeDate && (
                      <Tooltip title={formatDateTime(task.created)}>
                        <span>{relativeTime(task.created * 1000)}</span>
                      </Tooltip>
                    )}
                    {!relativeDate && (
                      <span>{formatDateTime(task.created)}</span>
                    )}
                  </TableCell>
                  <TableCell>
                    {task.status === 'in progress' && (
                      <span>{taskDuration(task.created, null)}</span>
                    )}
                    {task.status !== 'in progress' && (
                      <span>{taskDuration(task.created, task?.updated)}</span>
                    )}
                  </TableCell>
                  <TableCell>
                    {task.images.map((item, index) => {
                      return (
                        <div key={index}>
                          {item.image}:{item.tag}
                        </div>
                      );
                    })}
                  </TableCell>
                </TableRow>
                {task?.status_reason && visibleReasons.includes(task?.id) && (
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
            onPageChange && onPageChange(value);
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
          {itemsPerPageList.map(value => {
            return (
              <MenuItem key={value} value={value}>
                {value}
              </MenuItem>
            );
          })}
        </TextField>
      </Box>
      {deployLock && (
        <Box sx={{
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
        }}>
          <Typography variant="h6">Lockdown is active</Typography>
        </Box>
      )}
    </>
  );
}

export default TasksTable;
