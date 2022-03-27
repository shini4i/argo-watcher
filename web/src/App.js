import React, {useEffect, useState} from "react";
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

function App() {
  const [tasks, setTasks] = useState([]);
  const [loadingError, setLoadingError] = useState(null);
  const timeframes = {
    '5 minutes': 5 * 60,
    '15 minutes': 15 * 60,
    '30 minutes': 30 * 60,
    '1 hour': 60 * 60
  };
  const [timeframe, setTimeframe] = useState(timeframes['5 minutes']);

  const refreshTasks = (timeframe) => {
      let timestamp = Math.floor(Date.now() / 1000) - timeframe; // - 1h

      fetch(`/api/v1/tasks?timestamp=${timestamp}`)
          .then(res => {
              if (res.status !== 200) {
                  throw new Error(res.statusText);
              }
              return res.json();
          })
          .then(items => {
              setTasks(items);
          })
          .catch(error => {
              setLoadingError(error.message);
          })
      ;
  };

  useEffect(() => {
      refreshTasks(timeframe);
  }, []);


  const handleChange = (event) => {
    setTimeframe(event.target.value);
    refreshTasks(event.target.value);
  };

  return (
    <div>
        <Navbar />
        <Container maxWidth="xl">
            <Stack direction="row" spacing={2} alignItems="center">
                <Typography variant="h4" gutterBottom component="div" sx={{ flexGrow: 1 }}>
                    Existing tasks
                </Typography>
                <IconButton edge="start" color="inherit" onClick={() => {
                  refreshTasks(timeframe);
                }}>
                  <RefreshIcon />
                </IconButton>
                <Box sx={{ minWidth: 120 }}>
                    <FormControl fullWidth size={"small"}>
                        <InputLabel>Timeframe</InputLabel>
                        <Select
                            value={timeframe}
                            label="Timeframe"
                            onChange={handleChange}
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
                <Table sx={{ minWidth: 650 }} aria-label="simple table">
                    <TableHead>
                        <TableRow>
                            <TableCell>ID</TableCell>
                            <TableCell>Application</TableCell>
                            <TableCell>Project</TableCell>
                            <TableCell>Author</TableCell>
                            <TableCell>Status</TableCell>
                            <TableCell>Started</TableCell>
                            <TableCell>Updated</TableCell>
                            <TableCell>Images</TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {tasks.map((task) => (
                            <TableRow
                                key={task.id}
                                sx={{ '&:last-child td, &:last-child th': { border: 0 } }}
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
