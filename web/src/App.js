import React, {useEffect, useRef, useState} from "react";
import Navbar from "./Navbar";
import ErrorSnackbar from "./ErrorSnackbar";
import {relativeTime} from "./Utils";
import {fetchApplications, fetchTasks} from "./Services/Data";
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
import TextField from '@mui/material/TextField';
import Autocomplete from '@mui/material/Autocomplete';
import AdapterDateFns from '@mui/lab/AdapterDateFns';
import LocalizationProvider from '@mui/lab/LocalizationProvider';
import DatePicker from '@mui/lab/DatePicker';

const timeframes = {
  '5 minutes': 5 * 60,
  '15 minutes': 15 * 60,
  '30 minutes': 30 * 60,
  '1 hour': 60 * 60,
  '6 hours': 6 * 60 * 60,
  '12 hours': 12 * 60 * 60,
  'Custom': 0,
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
  const [currentApplication, setCurrentApplication] = useState(null);

  const [loadingError, setLoadingError] = useState(null);
  const [currentTimeframe, setCurrentTimeframe] = useState(timeframes['5 minutes']);
  const [currentCustomTimestamp, setCurrentCustomTimestamp] = useState(null);

  const calculateCurrentTimeframe = () => {
    if (currentTimeframe !== timeframes["Custom"]) {
      return currentTimeframe;
    } else if (currentCustomTimestamp !== null) {
      return Math.floor((Date.now() - currentCustomTimestamp.getTime()) / 1000);
    } else {
      return 0;
    }
  }

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
      refreshTasks(calculateCurrentTimeframe(), currentApplication);
    }, currentAutoRefresh * 1000);

    // clear interval on exit
    return () => {
      if (autoRefreshIntervalRef.current !== null) {
        clearInterval(autoRefreshIntervalRef.current);
      }
    };
  });

  const handleTimeframeChange = (event) => {
    let newTimeframe = event.target.value;
    setCurrentTimeframe(newTimeframe);
    if (newTimeframe !== timeframes["Custom"]) {
      refreshTasks(newTimeframe, currentApplication);
    } else if (currentCustomTimestamp !== null) {
      refreshTasks(Math.floor((Date.now() - currentCustomTimestamp.getTime()) / 1000), currentApplication);
    }
  };

  const handleAutoRefreshChange = (event) => {
    setCurrentAutoRefresh(event.target.value);
  };

  const handleApplicationsChange = (event, newValue) => {
    setCurrentApplication(newValue);
    refreshTasks(calculateCurrentTimeframe(), newValue);
  };

  const handleCustomTimestampChange = (newValue) => {
    if (!newValue) {
      return;
    }

    let newCustomTimestamp = new Date(newValue.getTime() - (newValue.getTime() % 86400000));
    setCurrentCustomTimestamp(newCustomTimestamp);

    if (currentTimeframe === timeframes["Custom"]) {
      refreshTasks(Math.floor((Date.now() - newCustomTimestamp.getTime()) / 1000), currentApplication);
    }
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
            <Box>
              <Autocomplete
                  size={"small"}
                  disablePortal
                  options={applications}
                  sx={{ width: 220 }}
                  renderInput={(params) => <TextField {...params} label="Application" />}
                  value={currentApplication || null}
                  onChange={handleApplicationsChange}
              />
            </Box>
            <IconButton edge="start" color="inherit" onClick={() => {
              refreshTasks(calculateCurrentTimeframe(), currentApplication);
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
            {currentTimeframe === timeframes['Custom'] &&  <Box sx={{width: 150}}>
              <LocalizationProvider dateAdapter={AdapterDateFns}>
                <DatePicker
                    renderInput={(props) => <TextField {...props} size="small" />}
                    label="Period Start Date"
                    value={currentCustomTimestamp}
                    maxDate={new Date()}
                    onChange={handleCustomTimestampChange}
                />
              </LocalizationProvider>
            </Box>}
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
