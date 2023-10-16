import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Typography from '@mui/material/Typography';
import {useEffect, useState} from 'react';
import {useParams} from 'react-router-dom';
import {fetchTask} from '../Services/Data';
import {useErrorContext} from '../ErrorContext';
import {Chip, Divider, Grid, Paper} from '@mui/material';
import {chipColorByStatus, formatDateTime, ProjectDisplay, StatusReasonDisplay,} from './TasksTable';

export default function TaskView() {
    const {id} = useParams();
    const [task, setTask] = useState(null);
    const {setError, setSuccess} = useErrorContext();

    useEffect(() => {
        fetchTask(id)
            .then((item) => {
                setSuccess('fetchTask', 'Fetched task successfully');
                setTask(item);
            })
            .catch((error) => {
                setError('fetchTasks', error.message);
            });
    }, [id]);

    return (
        <Container maxWidth="lg">
            <Paper elevation={3} sx={{padding: '20px', marginBottom: '20px'}}>
                <Typography
                    variant="h5"
                    gutterBottom
                    component="div"
                    sx={{display: 'flex', alignItems: 'center', marginBottom: '10px'}}
                >
                    <Box flexGrow={1}>Task Details</Box>
                    <Box fontSize="10px" color="gray">
                        UTC
                    </Box>
                </Typography>
                {!task && <Typography>Loading...</Typography>}
                {task && (
                    <Grid container spacing={2}>
                        <Grid item xs={12}>
                            <Typography variant="h6">Task Information</Typography>
                            <Divider/>
                        </Grid>
                        <Grid item xs={12} sm={6}>
                            <Typography variant="body2" color="textSecondary">
                                ID
                            </Typography>
                            <Typography variant="body1">{task.id}</Typography>
                        </Grid>
                        <Grid item xs={12} sm={6}>
                            <Typography variant="body2" color="textSecondary">
                                Created
                            </Typography>
                            <Typography variant="body1">
                                {formatDateTime(task.created)}
                            </Typography>
                        </Grid>
                        <Grid item xs={12} sm={6}>
                            <Typography variant="body2" color="textSecondary">
                                Updated
                            </Typography>
                            <Typography variant="body1">
                                {formatDateTime(task.updated)}
                            </Typography>
                        </Grid>
                        <Grid item xs={12} sm={6}>
                            <Typography variant="body2" color="textSecondary">
                                Application
                            </Typography>
                            <Typography variant="body1">{task.app}</Typography>
                        </Grid>
                        <Grid item xs={12} sm={6}>
                            <Typography variant="body2" color="textSecondary">
                                Author
                            </Typography>
                            <Typography variant="body1">{task.author}</Typography>
                        </Grid>
                        <Grid item xs={12} sm={6}>
                            <Typography variant="body2" color="textSecondary">
                                Project
                            </Typography>
                            <Typography variant="body1">
                                <ProjectDisplay project={task.project}/>
                            </Typography>
                        </Grid>
                        <Grid item xs={12} sm={6}>
                            <Typography variant="body2" color="textSecondary">
                                Status
                            </Typography>
                            <Chip
                                label={task.status}
                                color={chipColorByStatus(task.status)}
                            />
                        </Grid>
                        <Grid item xs={12} sm={12}>
                            <Typography variant="body2" color="textSecondary">
                                Status Details
                            </Typography>
                            <Typography variant="body1">
                                <StatusReasonDisplay reason={task.status_reason}/>
                            </Typography>
                        </Grid>
                        <Grid item xs={12}>
                            <Typography variant="h6">Images</Typography>
                            <Divider/>
                        </Grid>
                        {task.images.map((item, index) => (
                            <Grid item xs={12} sm={6} key={index}>
                                <Typography variant="body2" color="textSecondary">
                                    Image {index + 1}
                                </Typography>
                                <Typography variant="body1">
                                    {item.image}:{item.tag}
                                </Typography>
                            </Grid>
                        ))}
                    </Grid>
                )}
            </Paper>
        </Container>
    );
}
