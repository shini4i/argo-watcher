import {useEffect, useState} from "react";
import Navbar from "./Navbar";
import ErrorSnackbar from "./ErrorSnackbar";
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

function App() {
  const [tasks, setTasks] = useState([]);
  const [loadingError, setLoadingError] = useState(null);

  const refreshTasks = () => {
      fetch('/api/v1/tasks')
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
      refreshTasks();
  }, [])

  return (
    <div>
        <Navbar />
        <Container maxWidth="xl">
            <Stack direction="row" spacing={2} alignItems="center">
                <Typography variant="h4" gutterBottom component="div" sx={{ flexGrow: 1 }}>
                    Active tasks
                </Typography>
                <IconButton edge="start" color="inherit" onClick={refreshTasks}>
                    <RefreshIcon />
                </IconButton>
            </Stack>
            <TableContainer component={Paper}>
                <Table sx={{ minWidth: 650 }} aria-label="simple table">
                    <TableHead>
                        <TableRow>
                            <TableCell>ID</TableCell>
                            <TableCell>Application</TableCell>
                            <TableCell>Author</TableCell>
                            <TableCell>Status</TableCell>
                            <TableCell>Started</TableCell>
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
                                <TableCell>{task.author}</TableCell>
                                <TableCell>{task.status}</TableCell>
                                <TableCell>TODO</TableCell>
                                <TableCell>
                                    {task.images.map((item, index) => {
                                        return <div key={index}>{item.image}:{item.tag}</div>
                                    })}
                                </TableCell>
                            </TableRow>
                        ))}
                        {tasks.length === 0 && <TableRow>
                            <TableCell colSpan={100} sx={{textAlign: "center"}}>
                                No tasks are currently being executed
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
