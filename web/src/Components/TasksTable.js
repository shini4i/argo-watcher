import Table from '@mui/material/Table';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import Tooltip from '@mui/material/Tooltip';
import {
  relativeHumanDuration,
  relativeTime,
  relativeTimestamp,
} from '../Utils';
import TableContainer from '@mui/material/TableContainer';
import React, { useEffect, useState } from 'react';
import { fetchTasks } from '../Services/Data';
import Box from '@mui/material/Box';
import KeyboardArrowUpIcon from '@mui/icons-material/KeyboardArrowUp';
import KeyboardArrowDownIcon from '@mui/icons-material/KeyboardArrowDown';
import { Chip, Divider } from '@mui/material';
import Link from '@mui/material/Link';
import { addMinutes, format } from 'date-fns';
import Pagination from '@mui/material/Pagination';

const chipColorByStatus = status => {
  if (status === 'in progress') {
    return 'primary';
  }
  if (status === 'failed') {
    return 'error';
  }
  if (status === 'deployed') {
    return 'success';
  }
  return undefined;
};

const taskDuration = (created, updated) => {
  if (!updated) {
    updated = Math.round(Date.now() / 1000);
  }
  const seconds = updated - created;
  return relativeHumanDuration(seconds);
};

const formatDateTime = timestamp => {
  let dateTime = new Date(timestamp * 1000);
  return format(
    addMinutes(dateTime, dateTime.getTimezoneOffset()),
    'yyyy/MM/dd HH:mm:ss',
  );
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

function TasksTable({
  tasks,
  sortField,
  setSortField,
  relativeDate,
  onPageChange,
  page = 1,
}) {
  const pages = Math.ceil(tasks.length / 10);
  const tasksPaginated = tasks.slice((page - 1) * 10, page * 10);

  return (
    <>
      <TableContainer>
        <Table sx={{ minWidth: 650 }} aria-label="simple table">
          <TableHead>
            <TableRow>
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
              <TableRow
                key={task.id}
                sx={{ '&:last-child td, &:last-child th': { border: 0 } }}
              >
                <TableCell>{task.app}</TableCell>
                <TableCell>
                  {task.project.indexOf('http') === 0 ? (
                    <Link href={task.project}>
                      {task.project
                        .replace(/^http(s)?:\/\//, '')
                        .replace(/\/+$/, '')}
                    </Link>
                  ) : (
                    task.project
                  )}
                </TableCell>
                <TableCell>{task.author}</TableCell>
                <TableCell>
                  <Chip
                    label={task.status}
                    color={chipColorByStatus(task.status)}
                  />
                </TableCell>
                <TableCell>
                  {relativeDate && (
                    <Tooltip title={formatDateTime(task.created)}>
                      <span>{relativeTime(task.created * 1000)}</span>
                    </Tooltip>
                  )}
                  {!relativeDate && <span>{formatDateTime(task.created)}</span>}
                </TableCell>
                <TableCell>
                  {task.updated && (
                    <span>{taskDuration(task.created, task.updated)}</span>
                  )}
                  {!task.updated && <span>-</span>}
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
            ))}
            {tasks.length === 0 && (
              <TableRow>
                <TableCell colSpan={100} sx={{ textAlign: 'center' }}>
                  No tasks were found within provided timeframe
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </TableContainer>
      {pages > 1 && (
        <>
          <Divider />
          <Box sx={{ m: 1, display: 'flex', justifyContent: 'center' }}>
            <Pagination
              count={Math.ceil(tasks.length / 10)}
              variant="outlined"
              shape="rounded"
              page={page}
              onChange={(_event, value) => {
                onPageChange && onPageChange(value);
              }}
            />
          </Box>
        </>
      )}
    </>
  );
}

export default TasksTable;
