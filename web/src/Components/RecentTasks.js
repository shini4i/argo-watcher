import React, {useEffect, useRef, useState} from "react";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import IconButton from "@mui/material/IconButton";
import RefreshIcon from "@mui/icons-material/Refresh";
import FormControl from "@mui/material/FormControl";
import InputLabel from "@mui/material/InputLabel";
import Select from "@mui/material/Select";
import MenuItem from "@mui/material/MenuItem";
import ErrorSnackbar from "./ErrorSnackbar";
import ApplicationsFilter from "./ApplicationsFilter";
import TasksTable, {useTasks} from "./TasksTable";

const autoRefreshIntervals = {
  '5s': 5,
  '10s': 10,
  '30s': 30,
  '1m': 60,
  'off': 0,
};

function RecentTasks() {
  const [loadingError, setLoadingError] = useState(null);
  const {tasks, sortField, setSortField, refreshTasksInTimeframe} = useTasks({ setLoadingError });
  const [currentAutoRefresh, setCurrentAutoRefresh] = useState(autoRefreshIntervals['30s']);
  const autoRefreshIntervalRef = useRef(null);
  const [currentApplication, setCurrentApplication] = useState(null);
  const currentTimeframe = 9 * 60 * 60;

  // initial load
  useEffect(() => {
    refreshTasksInTimeframe(currentTimeframe, currentApplication);
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
      refreshTasksInTimeframe(currentTimeframe, currentApplication);
    }, currentAutoRefresh * 1000);

    // clear interval on exit
    return () => {
      if (autoRefreshIntervalRef.current !== null) {
        clearInterval(autoRefreshIntervalRef.current);
      }
    };
  });

  const handleAutoRefreshChange = (event) => {
    setCurrentAutoRefresh(event.target.value);
  };

  return (
    <Container maxWidth="xl">
      <Stack direction="row" spacing={2} alignItems="center">
        <Typography variant="h4" gutterBottom component="div" sx={{flexGrow: 1}}>
          Recent tasks
        </Typography>
        <Box>
          <ApplicationsFilter
            value={currentApplication}
            onChange={(value) => {
              setCurrentApplication(value);
              refreshTasksInTimeframe(currentTimeframe, value);
            }}
            setLoadingError={setLoadingError}
          />
        </Box>
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
        <IconButton edge="start" color={"primary"} title={"force table load"} onClick={() => {
          refreshTasksInTimeframe(currentTimeframe, currentApplication);
        }}>
          <RefreshIcon/>
        </IconButton>
      </Stack>
      <TasksTable
          tasks={tasks}
          sortField={sortField}
          setSortField={setSortField}
      />
      <ErrorSnackbar message={loadingError} setMessage={setLoadingError}/>
    </Container>
  );
}

export default RecentTasks;
