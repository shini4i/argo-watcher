import Paper from "@mui/material/Paper";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import Tooltip from "@mui/material/Tooltip";
import {relativeTime} from "../Utils";
import TableContainer from "@mui/material/TableContainer";
import React, {useEffect, useState} from "react";
import {fetchTasks} from "../Services/Data";
import Box from "@mui/material/Box";
import KeyboardArrowUpIcon from "@mui/icons-material/KeyboardArrowUp";
import KeyboardArrowDownIcon from "@mui/icons-material/KeyboardArrowDown";

export function useTasks({ setLoadingError }) {
  const [tasks, setTasks] = useState([]);
  const [sortField, setSortField] = useState({field: "created", direction: "ASC"});

  const refreshTasks = (timeframe, application) => {
    let timestamp = Math.floor(Date.now() / 1000) - timeframe;
    // get tasks by timestamp
    fetchTasks(timestamp, application)
        .then(items => { setTasksSorted(items, sortField); })
        .catch(error => { setLoadingError(error.message); });
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
    setTasksSorted(tasks, sortField)
  }, [sortField]);

  return {
    tasks, sortField, setSortField, refreshTasks
  }
}

function TableCellSorted({field, sortField, setSortField, children}) {
  const triggerSortChange = (triggerField) => {
    // change sort parameters
    let sortFieldChange = {...sortField};
    if (sortFieldChange.field === triggerField) {
      sortFieldChange.direction = sortFieldChange.direction === 'ASC' ? 'DESC' : 'ASC';
    } else {
      sortFieldChange.field = triggerField;
      sortFieldChange.direction = 'ASC';
    }
    setSortField(sortFieldChange);
  };

  return <TableCell
      onClick={() => {  triggerSortChange(field); }}
      sx={{cursor: "pointer",}}
  >
    <Box sx={{display: "flex", flexDirection: "row",}}>
      {children} {sortField.field === field && (sortField.direction === "ASC" ? <KeyboardArrowUpIcon /> : <KeyboardArrowDownIcon />)}
    </Box>
  </TableCell>
}

function TasksTable({ tasks, sortField, setSortField }) {
  return (
      <TableContainer component={Paper}>
        <Table sx={{minWidth: 650}} aria-label="simple table">
          <TableHead>
            <TableRow>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={"id"}>ID</TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={"app"}>Application</TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={"project"}>Project</TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={"author"}>Author</TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={"status"}>Status</TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={"created"}>Started</TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={"updated"}>Updated</TableCellSorted>
              <TableCellSorted sortField={sortField} setSortField={setSortField} field={"images"}>Images</TableCellSorted>
            </TableRow>
          </TableHead>
          <TableBody>
            {tasks.map((task) => (
                <TableRow
                    key={task.id}
                    sx={{'&:last-child td, &:last-child th': {border: 0}}}
                >
                  <TableCell component="th" scope="row">
                    {task.id}
                  </TableCell>
                  <TableCell>{task.app}</TableCell>
                  <TableCell>{task.project}</TableCell>
                  <TableCell>{task.author}</TableCell>
                  <TableCell>{task.status}</TableCell>
                  <TableCell>
                    <Tooltip title={new Date(task.created * 1000).toISOString()}>
                      <span>{relativeTime(task.created * 1000)}</span>
                    </Tooltip>
                  </TableCell>
                  <TableCell>
                    <Tooltip title={new Date(task.updated * 1000).toISOString()}>
                      <span>{relativeTime(task.updated * 1000)}</span>
                    </Tooltip>
                  </TableCell>
                  <TableCell>
                    {task.images.map((item, index) => {
                      return <div key={index}>{item.image}:{item.tag}</div>
                    })}
                  </TableCell>
                </TableRow>
            ))}
            {tasks.length === 0 && <TableRow>
              <TableCell colSpan={100} sx={{textAlign: "center"}}>
                No tasks were found within provided timeframe
              </TableCell>
            </TableRow>}
          </TableBody>
        </Table>
      </TableContainer>
  );
}

export default TasksTable;
