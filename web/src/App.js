import {useEffect, useState} from "react";

import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Navbar from "./Navbar";
import Container from "@mui/material/Container";
import Stack from "@mui/material/Stack";
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Paper from '@mui/material/Paper';
import RefreshIcon from '@mui/icons-material/Refresh';

function App() {
  const [tasks, setTasks] = useState([]);

  const refreshTasks = () => {
      fetch('/api/v1/tasks')
          .then(res => res.json())
          .then(items => {
              setTasks(items);
          });
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
                        {tasks.map((row) => (
                            <TableRow
                                key={row.name}
                                sx={{ '&:last-child td, &:last-child th': { border: 0 } }}
                            >
                                <TableCell component="th" scope="row">
                                    {row.id}
                                </TableCell>
                                <TableCell>{row.app}</TableCell>
                                <TableCell>{row.author}</TableCell>
                                <TableCell>{row.status}</TableCell>
                                <TableCell>TODO</TableCell>
                                <TableCell>
                                    {row.images.map(item => {
                                        return <div>{item.image}:{item.tag}</div>
                                    })}
                                </TableCell>
                            </TableRow>
                        ))}
                        {tasks.length === 0 && <TableRow>
                            <TableCell colSpan={100} sx={{textAlign: "center"}}>
                                No tasks currently is being executed
                            </TableCell>
                        </TableRow>}
                    </TableBody>
                </Table>
            </TableContainer>
        </Container>
    </div>
  );
}

export default App;
