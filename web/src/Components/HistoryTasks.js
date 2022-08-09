import React, {forwardRef, useEffect, useState} from "react";
import { useSearchParams } from "react-router-dom";
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
  const [searchParams, setSearchParams] = useSearchParams();
  const [loadingError, setLoadingError] = useState(null);
  const {
      tasks,
      sortField,
      setSortField,
      refreshTasksInRange,
      clearTasks
    } = useTasks({ setLoadingError });
  const [currentApplication, setCurrentApplication] = useState(searchParams.get('app') ?? null);
  const [dateRange, setDateRange] = useState([
    // start date
    Number(searchParams.get('start'))
        ? new Date(Number(searchParams.get('start')) * 1000)
        : startOfDay(new Date()),
    // end date
    Number(searchParams.get('end'))
        ? new Date(Number(searchParams.get('end')) * 1000)
        : startOfDay(new Date())
  ]);
  const [startDate, endDate] = dateRange;

  const refreshWithFilters = (start, end, application) => {
   if (start && end) {
      // re-fetch tasks
      refreshTasksInRange(
          Math.floor(startOfDay(start).getTime() / 1000),
          Math.floor(endOfDay(end).getTime() / 1000),
          application
      );
      // save to filters
     setSearchParams({
       app: application ?? "",
       start: Math.floor(start.getTime()/1000),
       end: Math.floor(end.getTime()/1000),
     });
   } else {
      // reset list of tasks
      clearTasks();
    }
  };

  useEffect(() => {
    refreshWithFilters(startDate, endDate, currentApplication);
  }, []);

  const DateRangePickerCustomInput = forwardRef(({ value, onClick }, ref) => (
      <TextField size="small" sx={{minWidth: "220px"}} onClick={onClick} ref={ref} value={value} label={"Date range"}/>
  ));

  return (
    <Container maxWidth="xl">
      <Stack direction="row" spacing={2} alignItems="center">
        <Typography variant="h4" gutterBottom component="div" sx={{flexGrow: 1, display: 'flex', gap: '10px'}}>
          <Box>History tasks</Box>
          <Box sx={{fontSize: '10px'}}>UTC</Box>
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
          relativeDate={false}
      />
      <ErrorSnackbar message={loadingError} setMessage={setLoadingError}/>
    </Container>
  );
}

export default HistoryTasks;
