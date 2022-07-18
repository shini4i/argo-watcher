import React, {forwardRef, useEffect, useState} from "react";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import TextField from "@mui/material/TextField";
import IconButton from "@mui/material/IconButton";
import RefreshIcon from "@mui/icons-material/Refresh";
import ErrorSnackbar from "./ErrorSnackbar";
import ApplicationsFilter from "./ApplicationsFilter";
import TasksTable, {useTasks} from "./TasksTable";
import {endOfDay, startOfDay} from 'date-fns'
import DatePicker from "react-datepicker";
import "react-datepicker/dist/react-datepicker.css";

function HistoryTasks() {
  const [loadingError, setLoadingError] = useState(null);
  const {
      tasks,
      sortField,
      setSortField,
      refreshTasksInRange,
      clearTasks
    } = useTasks({ setLoadingError });
  const [currentApplication, setCurrentApplication] = useState(null);
  const [dateRange, setDateRange] = useState([startOfDay(new Date).getTime(), startOfDay(new Date).getTime()]);
  const [startDate, endDate] = dateRange;

  const refreshWithFilters = (startDate, endDate, application) => {
   if (startDate && endDate) {
      // use absolute time
      refreshTasksInRange(
          Math.floor(startOfDay(new Date(startDate)).getTime() / 1000),
          Math.floor(endOfDay(new Date(endDate)).getTime() / 1000),
          application
      );
    } else {
      // reset list of tasks
      clearTasks();
    }
  };

  useEffect(() => {
    refreshWithFilters(startDate, endDate, currentApplication);
  }, []);

  const DateRangePickerCustomInput = forwardRef(({ value, onClick }, ref) => (
      <TextField size="small" sx={{minWidth: "220px"}} onClick={onClick} ref={ref} value={value} />
  ));

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
                refreshWithFilters(startDate, endDate, value);
              }}
              setLoadingError={setLoadingError}
          />
        </Box>
        <Box>
          <DatePicker
              selectsRange={true}
              startDate={startDate}
              endDate={endDate}
              onChange={(update) => {
                setDateRange(update);
                refreshWithFilters(update[0], update[1], currentApplication);
              }}
              maxDate={new Date()}
              isClearable={false}
              customInput={<DateRangePickerCustomInput />}
              required
            />
        </Box>
        <IconButton edge="start" color={"primary"} title={"reload table"} onClick={() => {
          refreshWithFilters(startDate, endDate, currentApplication);
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
