import React, {useEffect, useState} from "react";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import TextField from "@mui/material/TextField";
import IconButton from "@mui/material/IconButton";
import RefreshIcon from "@mui/icons-material/Refresh";
import FormControl from "@mui/material/FormControl";
import InputLabel from "@mui/material/InputLabel";
import Select from "@mui/material/Select";
import MenuItem from "@mui/material/MenuItem";
import LocalizationProvider from "@mui/lab/LocalizationProvider";
import AdapterDateFns from "@mui/lab/AdapterDateFns";
import DatePicker from "@mui/lab/DatePicker";
import ErrorSnackbar from "./ErrorSnackbar";
import ApplicationsFilter from "./ApplicationsFilter";
import TasksTable, {useTasks} from "./TasksTable";

const timeframes = {
  '5 minutes': 5 * 60,
  '15 minutes': 15 * 60,
  '30 minutes': 30 * 60,
  '1 hour': 60 * 60,
  '6 hours': 6 * 60 * 60,
  '12 hours': 12 * 60 * 60,
  'Custom': 0,
};

function HistoryTasks() {
  const [loadingError, setLoadingError] = useState(null);
  const {tasks, sortField, setSortField, refreshTasks} = useTasks({ setLoadingError });
  const [currentApplication, setCurrentApplication] = useState(null);
  const [currentTimeframe, setCurrentTimeframe] = useState(timeframes['1 hour']);
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

  const handleTimeframeChange = (event) => {
    let newTimeframe = event.target.value;
    setCurrentTimeframe(newTimeframe);
    if (newTimeframe !== timeframes["Custom"]) {
      refreshTasks(newTimeframe, currentApplication);
    } else if (currentCustomTimestamp !== null) {
      refreshTasks(Math.floor((Date.now() - currentCustomTimestamp.getTime()) / 1000), currentApplication);
    }
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

  useEffect(() => {
    refreshTasks(currentTimeframe, currentApplication);
  }, []);

  return (
    <Container maxWidth="xl">
      <Stack direction="row" spacing={2} alignItems="center">
        <Typography variant="h4" gutterBottom component="div" sx={{flexGrow: 1}}>
          History tasks
        </Typography>
        <Box>
          <ApplicationsFilter
              value={currentApplication}
              onChange={(value) => {
                setCurrentApplication(value);
                refreshTasks(calculateCurrentTimeframe(), value);
              }}
              setLoadingError={setLoadingError}
          />
        </Box>
        {currentTimeframe === timeframes['Custom'] &&  <Box sx={{width: 200}}>
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
        <IconButton edge="start" color={"primary"} title={"reload table"} onClick={() => {
          refreshTasks(calculateCurrentTimeframe(), currentApplication);
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

export default HistoryTasks;
