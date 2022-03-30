import React, {useEffect, useRef, useState} from "react";
import Navbar from "./Navbar";
import ErrorSnackbar from "./ErrorSnackbar";
import {relativeTime} from "./Utils";
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Container from "@mui/material/Container";
import Stack from "@mui/material/Stack";
import Paper from '@mui/material/Paper';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import RefreshIcon from '@mui/icons-material/Refresh';
import Tooltip from "@mui/material/Tooltip";
import Box from '@mui/material/Box';
import InputLabel from '@mui/material/InputLabel';
import MenuItem from '@mui/material/MenuItem';
import FormControl from '@mui/material/FormControl';
import Select from '@mui/material/Select';
import KeyboardArrowUpIcon from '@mui/icons-material/KeyboardArrowUp';
import KeyboardArrowDownIcon from '@mui/icons-material/KeyboardArrowDown';
import {fetchApplications, fetchTasks} from "./Services/Data";

const timeframes = {
  '5 minutes': 5 * 60,
  '15 minutes': 15 * 60,
  '30 minutes': 30 * 60,
  '1 hour': 60 * 60
};

const autoRefreshIntervals = {
  '5s': 5,
  '10s': 10,
  '30s': 30,
  '1m': 60,
  'off': 0,
};

function App() {
  const [tasks, setTasks] = useState([]);
  const [applications, setApplications] = useState([]);

  const [currentSort, setCurrentSort] = useState({field: "created", direction: "ASC"});
  const [currentAutoRefresh, setCurrentAutoRefresh] = useState(autoRefreshIntervals['30s']);
  const autoRefreshIntervalRef = useRef(null);
  const [currentApplication, setCurrentApplication] = useState("");

  const [loadingError, setLoadingError] = useState(null);
  const [currentTimeframe, setCurrentTimeframe] = useState(timeframes['5 minutes']);

  const refreshTasks = (timeframe, application) => {
    let timestamp = Math.floor(Date.now() / 1000) - timeframe;
    // get tasks by timestamp
    fetchTasks(timestamp, application)
        .then(items => { setTasksSorted(items, currentSort); })
        .catch(error => { setLoadingError(error.message); });
  };

  const setTasksSorted = (tasks, sort) => {
    // sort tasks
    tasks.sort((a, b) => {
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
    setTasks([].concat(tasks));
  };

  const triggerSortChange = (field) => {
    // change sort parameters
    let sortFieldChange = {...currentSort};
    if (sortFieldChange.field === field) {
      sortFieldChange.direction = sortFieldChange.direction === 'ASC' ? 'DESC' : 'ASC';
    } else {
      sortFieldChange.field = field;
      sortFieldChange.direction = 'ASC';
    }
    setCurrentSort(sortFieldChange);
    // set sorted tasks
    setTasksSorted(tasks, sortFieldChange);
  };

  // initial load
  useEffect(() => {
    refreshTasks(currentTimeframe, currentApplication);
    fetchApplications()
        .then(items => { setApplications(items) })
        .catch(error => { setLoadingError(error.message); });
  }, []);

  // we reset interval on any state change (because we use the state variables for data retrieval)
  useEffect(() => {
    // reset current interval
    if (autoRefreshIntervalRef.current !== null) {
      clearInterval(autoRefreshIntervalRef.current);
    }
    if (!currentAutoRefresh) { // value is 0 for "off"
      return;
    }
    // set interval
    autoRefreshIntervalRef.current = setInterval(() => {
      refreshTasks(currentTimeframe, currentApplication);
    }, currentAutoRefresh * 1000);

    // clear interval on exit
    return () => {
      if (autoRefreshIntervalRef.current !== null) {
        clearInterval(autoRefreshIntervalRef.current);
      }
    };
  });

  const handleTimeframeChange = (event) => {
    setCurrentTimeframe(event.target.value);
    refreshTasks(event.target.value, currentApplication);
  };

  const handleAutoRefreshChange = (event) => {
    setCurrentAutoRefresh(event.target.value);
  };

  const handleApplicationsChange = (event) => {
    setCurrentApplication(event.target.value);
    refreshTasks(currentTimeframe, event.target.value);
  };

  const TableCellSorted = ({field, children}) => {
    return <TableCell
      onClick={() => {  triggerSortChange(field); }}
      sx={{cursor: "pointer",}}
    >
      <Box sx={{display: "flex", flexDirection: "row",}}>
        {children} {currentSort.field === field && (currentSort.direction === "ASC" ? <KeyboardArrowUpIcon /> : <KeyboardArrowDownIcon />)}
      </Box>
    </TableCell>
  };

  return (
      <div>
        <Navbar/>
        <Container maxWidth="xl">
          <Stack direction="row" spacing={2} alignItems="center">
            <Typography variant="h4" gutterBottom component="div" sx={{flexGrow: 1}}>
              Existing tasks
            </Typography>
            <Box sx={{minWidth: 140}}>
              <FormControl fullWidth size={"small"}>
                <InputLabel>Applications</InputLabel>
                <Select
                    value={currentApplication}
                    label="Applications"
                    onChange={handleApplicationsChange}
                >
                  <MenuItem value="">
                    <em>None</em>
                  </MenuItem>
                  {applications.map(application => {
                    return <MenuItem key={application} value={application}>{application}</MenuItem>
                  })}
                </Select>
              </FormControl>
            </Box>
            <IconButton edge="start" color="inherit" onClick={() => {
              refreshTasks(currentTimeframe, currentApplication);
            }}>
              <RefreshIcon/>
            </IconButton>
            <Box sx={{minWidth: 120}}>
              <FormControl fullWidth size={"small"}>
                <InputLabel>Auto-Refresh</InputLabel>
                <Select
                    value={currentAutoRefresh}
                    label="Auto-Refresh"
                    onChange={handleAutoRefreshChange}
                >
                  {Object.keys(autoRefreshIntervals).map(autoRefreshInterval => {
                    let value = autoRefreshIntervals[autoRefreshInterval];
                    return <MenuItem key={autoRefreshInterval} value={value}>{autoRefreshInterval}</MenuItem>
                  })}
                </Select>
              </FormControl>
            </Box>
            <Box sx={{minWidth: 120}}>
              <FormControl fullWidth size={"small"}>
                <InputLabel>Timeframe</InputLabel>
                <Select
                    value={currentTimeframe}
                    label="Timeframe"
                    onChange={handleTimeframeChange}
                >
                  {Object.keys(timeframes).map(timeframe => {
                    let value = timeframes[timeframe];
                    return <MenuItem key={timeframe} value={value}>{timeframe}</MenuItem>
                  })}
                </Select>
              </FormControl>
            </Box>
          </Stack>
          <TableContainer component={Paper}>
            <Table sx={{minWidth: 650}} aria-label="simple table">
              <TableHead>
                <TableRow>
                  <TableCellSorted field={"id"}>ID</TableCellSorted>
                  <TableCellSorted field={"app"}>Application</TableCellSorted>
                  <TableCellSorted field={"project"}>Project</TableCellSorted>
                  <TableCellSorted field={"author"}>Author</TableCellSorted>
                  <TableCellSorted field={"status"}>Status</TableCellSorted>
                  <TableCellSorted field={"created"}>Started</TableCellSorted>
                  <TableCellSorted field={"updated"}>Updated</TableCellSorted>
                  <TableCellSorted field={"images"}>Images</TableCellSorted>
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
          <ErrorSnackbar message={loadingError} setMessage={setLoadingError}/>
        </Container>
      </div>
  );
}

export default App;
