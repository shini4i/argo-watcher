import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Container from '@mui/material/Container';
import Typography from '@mui/material/Typography';
import React, { useContext, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { fetchTask } from '../Services/Data';
import { useErrorContext } from '../ErrorContext';
import {
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Divider,
  Grid,
  Paper,
} from '@mui/material';
import { formatDateTime, ProjectDisplay, StatusReasonDisplay } from './TasksTable';
import { AuthContext } from '../auth';
import { fetchConfig } from '../config';
import { useDeployLock } from '../deployLockHandler';
import CheckCircleOutlineIcon from '@mui/icons-material/CheckCircleOutline';
import CancelOutlinedIcon from '@mui/icons-material/CancelOutlined';
import CircularProgress from '@mui/material/CircularProgress';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';
import Tooltip from '@mui/material/Tooltip';

export default function TaskView() {
  const { id } = useParams();
  const [task, setTask] = useState(null);
  const { setError, setSuccess } = useErrorContext();
  const { authenticated, email, groups, privilegedGroups, keycloakToken } = useContext(AuthContext);
  const [configData, setConfigData] = useState(null);
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);

  const handleClickOpen = () => {
    setOpen(true);
  };

  const handleClose = () => {
    setOpen(false);
  };

  const handleConfirm = async () => {
    setOpen(false);
    await rollbackToVersion();
  };

  const deployLock = useDeployLock();

  useEffect(() => {
    fetchConfig().then(config => {
      setConfigData(config);
    });
  }, []);

  const getArgoCDUrl = () => {
    if (configData?.argo_cd_url_alias) {
      return `${configData.argo_cd_url_alias}/applications/${task.app}`;
    } else if (configData?.argo_cd_url) {
      return `${configData.argo_cd_url.Scheme}://${configData.argo_cd_url.Host}${configData.argo_cd_url.Path}/applications/${task.app}`;
    }
    return '';
  };

  useEffect(() => {
    fetchTask(id)
      .then(item => {
        setSuccess('fetchTask', 'Fetched task successfully');
        setTask(item);
      })
      .catch(error => {
        setError('fetchTasks', error.message);
      });
  }, [id]);

  useEffect(() => {
    let intervalId;

    const fetchTaskStatus = async () => {
      console.log('Fetching task status...');
      try {
        const updatedTask = await fetchTask(id);
        setTask(updatedTask);
      } catch (error) {
        setError('fetchTaskStatus', error.message);
      }
    };

    if (task && task.status === 'in progress') {
      intervalId = setInterval(fetchTaskStatus, 10000);
    }

    return () => {
      if (intervalId) {
        clearInterval(intervalId);
      }
    };
  }, [task]);

  const userIsPrivileged = groups && privilegedGroups && groups.some(group => privilegedGroups.includes(group));

  const rollbackToVersion = async () => {
    const updatedTask = {
      ...task,
      author: email,
    };

    try {
      const response = await fetch(`${window.location.origin}/api/v1/tasks`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': keycloakToken,
        },
        body: JSON.stringify(updatedTask),
      });

      if (response.status === 401) { // HTTP 401 Unauthorized
        throw new Error('You are not authorized to perform this action!');
      } else if (response.status === 406) { // HTTP 406 Not Acceptable
        throw new Error('Lockdown is active. Deployments are forbidden!');
      } else if (response.status !== 202) { // HTTP 202 Accepted
        throw new Error(`Received unexpected status code: ${response.status}`);
      }

      navigate('/');
    } catch (error) {
      setError('fetchTasks', error.message);
    }
  };

  return (
    <Container maxWidth="lg">
      <Paper elevation={3} sx={{ padding: '20px', marginBottom: '20px' }}>
        <Typography
          variant="h5"
          gutterBottom
          component="div"
          sx={{ display: 'flex', alignItems: 'center', marginBottom: '10px' }}
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
              <Divider />
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
                <ProjectDisplay project={task.project} />
              </Typography>
            </Grid>
            <Grid item xs={12} sm={6}>
              <Typography variant="body2" color="textSecondary">
                Status
              </Typography>
              {task.status === 'deployed' && (
                <Tooltip title="Deployed">
                  <CheckCircleOutlineIcon style={{ color: 'green' }} />
                </Tooltip>
              )}
              {task.status === 'failed' && (
                <Tooltip title="Failed">
                  <CancelOutlinedIcon style={{ color: 'red' }} />
                </Tooltip>
              )}
              {task.status === 'in progress' && (
                <Tooltip title="In Progress">
                  <CircularProgress />
                </Tooltip>
              )}
              {task.status === 'app not found' && (
                <Tooltip title="App Not Found">
                  <ErrorOutlineIcon style={{ color: 'gray' }} />
                </Tooltip>
              )}
            </Grid>
            {task.status_reason && (
              <Grid item xs={12} sm={12}>
                <Typography variant="body2" color="textSecondary">
                  Status Details
                </Typography>
                <Typography variant="body1">
                  <StatusReasonDisplay reason={task.status_reason} />
                </Typography>
              </Grid>
            )}
            <Grid item xs={12}>
              <Typography variant="h6">Images</Typography>
              <Divider />
            </Grid>
            {task.images.map((item, index) => (
              <Grid item xs={12} sm={6} key={`${item.image}:${item.tag}`}>
                <Typography variant="body2" color="textSecondary">
                  Image {index + 1}
                </Typography>
                <Typography variant="body1">
                  {item.image}:{item.tag}
                </Typography>
              </Grid>
            ))}
            <Grid item xs={12}>
              <a href={getArgoCDUrl()} target="_blank" rel="noopener noreferrer">
                <Button variant="contained" color="primary" style={{ marginRight: '10px' }}>
                  Open in ArgoCD UI
                </Button>
              </a>
              {authenticated && userIsPrivileged && (
                <Button
                  variant="contained"
                  color="primary"
                  onClick={handleClickOpen}
                  disabled={task.status === 'in progress'}
                >
                  Rollback to this version
                </Button>
              )}
              <Dialog
                open={open}
                onClose={handleClose}
                aria-labelledby="alert-dialog-title"
                aria-describedby="alert-dialog-description"
              >
                <DialogTitle id="alert-dialog-title">{'Rollback Confirmation'}</DialogTitle>
                <DialogContent>
                  <DialogContentText id="alert-dialog-description">
                    Are you sure you want to rollback to this version?
                  </DialogContentText>
                </DialogContent>
                <DialogActions>
                  <Button onClick={handleClose} color="primary">
                    Cancel
                  </Button>
                  <Button onClick={handleConfirm} color="primary" autoFocus>
                    Yes
                  </Button>
                </DialogActions>
              </Dialog>
            </Grid>
          </Grid>
        )}
      </Paper>
      {deployLock && (
        <Box sx={{
          position: 'fixed',
          bottom: 0,
          left: 0,
          width: '100%',
          backgroundColor: 'error.main',
          color: 'white',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          py: 2,
        }}>
          <Typography variant="h6">Lockdown is active</Typography>
        </Box>
      )}
    </Container>
  );
}
