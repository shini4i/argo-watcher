import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Stack from '@mui/material/Stack';
import Typography from '@mui/material/Typography';
import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { fetchTask } from '../Services/Data';
import { useErrorContext } from '../ErrorContext';
import { Grid, Chip } from '@mui/material';
import {
  chipColorByStatus,
  formatDateTime,
  ProjectDisplay,
  StatusReasonDisplay,
} from './TasksTable';
export default function TaskView() {
  const { id } = useParams();
  const [task, setTask] = useState(null);
  const { setError, setSuccess } = useErrorContext();

  useEffect(() => {
    fetchTask(id)
      .then(item => {
        setSuccess('fetchTask', 'Fetched tas successfully');
        setTask(item);
      })
      .catch(error => {
        setError('fetchTasks', error.message);
      });
  }, [id]);

  return (
    <Container maxWidth="lg">
      <Stack
        direction={{ xs: 'column', md: 'row' }}
        spacing={2}
        alignItems="center"
        sx={{ mb: 2 }}
      >
        <Typography
          variant="h5"
          gutterBottom
          component="div"
          sx={{ flexGrow: 1, display: 'flex', gap: '10px' }}
        >
          <Box>Task details</Box>
          <Box sx={{ fontSize: '10px' }}>UTC</Box>
        </Typography>
      </Stack>
      {!task && <Typography>Loading...</Typography>}
      {task && (
        <Grid container spacing={3}>
          <Grid item xs={3}>
            <Typography>Id</Typography>
          </Grid>
          <Grid item xs={9}>
            <Typography variant="body2" sx={{ color: 'neutral.main' }}>
              {task.id}
            </Typography>
          </Grid>
          <Grid item xs={3}>
            <Typography>Created</Typography>
          </Grid>
          <Grid item xs={9}>
            <Typography variant="body2">
              <span>{formatDateTime(task.created)}</span>
            </Typography>
          </Grid>
          <Grid item xs={3}>
            <Typography>Updated</Typography>
          </Grid>
          <Grid item xs={9}>
            <Typography variant="body2">
              <span>{task.updated ? formatDateTime(task.updated) : '---'}</span>
            </Typography>
          </Grid>
          <Grid item xs={3}>
            <Typography>Application</Typography>
          </Grid>
          <Grid item xs={9}>
            <Typography variant="body2">{task.app}</Typography>
          </Grid>
          <Grid item xs={3}>
            <Typography>Author</Typography>
          </Grid>
          <Grid item xs={9}>
            <Typography variant="body2">{task.author}</Typography>
          </Grid>
          <Grid item xs={3}>
            <Typography>Project</Typography>
          </Grid>
          <Grid item xs={9}>
            <ProjectDisplay project={task.project}></ProjectDisplay>
          </Grid>
          <Grid item xs={3}>
            <Typography>Images</Typography>
          </Grid>
          <Grid item xs={9}>
            {task.images.map((item, index) => {
              return (
                <div key={index}>
                  <Typography variant="body2">
                    {item.image}:{item.tag}
                  </Typography>
                </div>
              );
            })}
          </Grid>
          <Grid item xs={3}>
            <Typography>Status</Typography>
          </Grid>
          <Grid item xs={9}>
            <Chip label={task.status} color={chipColorByStatus(task.status)} />
          </Grid>
          <Grid item xs={3}>
            <Typography>Status details</Typography>
          </Grid>
          <Grid item xs={9}>
            <StatusReasonDisplay reason={task.status_reason} />
          </Grid>
        </Grid>
      )}
    </Container>
  );
}
