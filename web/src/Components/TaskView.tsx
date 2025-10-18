import React, { useContext, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
  Box,
  Button,
  CircularProgress,
  Container,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Divider,
  Grid,
  Paper,
  Tooltip,
  Typography,
} from '@mui/material';
import CheckCircleOutlineIcon from '@mui/icons-material/CheckCircleOutline';
import CancelOutlinedIcon from '@mui/icons-material/CancelOutlined';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';

import { fetchConfig, fetchTask } from '../Services/Data';
import { useErrorContext } from '../ErrorContext';
import { formatDateTime, ProjectDisplay, StatusReasonDisplay } from './TasksTable';
import { AuthContext } from '../Services/Auth';
import { useDeployLock } from '../Services/DeployLockHandler';

interface Task {
  id: string;
  created: string;
  updated: string;
  app: string;
  author: string;
  project: string;
  status: string;
  status_reason?: string;
  images: Array<{ image: string; tag: string }>;
}

interface ConfigData {
  argo_cd_url_alias?: string;
  argo_cd_url?: { Scheme: string; Host: string; Path: string };
}

interface ConfigType {
  keycloak: {
    enabled: boolean;
    url: string;
    realm: string;
    client_id: string;
    privileged_groups: string[];
    token_validation_interval: number;
  };
  argo_cd_url_alias?: string;
  argo_cd_url?: { Scheme: string; Host: string; Path: string };
}

/**
 * Type guard ensuring the received value matches the Task structure expected by the view.
 *
 * @param value Arbitrary value returned from the API.
 * @returns True when the value satisfies the Task shape.
 */
const isTask = (value: unknown): value is Task => {
  if (!value || typeof value !== 'object') {
    return false;
  }
  const candidate = value as Record<string, unknown>;
  return ['id', 'created', 'updated', 'app', 'author', 'project', 'status', 'images'].every(
    (key) => key in candidate
  );
};

export default function TaskView() {
  const { id } = useParams<{ id: string }>();
  const [task, setTask] = useState<Task | null>(null);
  const { setError, setSuccess } = useErrorContext();
  const authContext = useContext(AuthContext);

  if (!authContext) {
    throw new Error('AuthContext must be used within an AuthProvider');
  }

  const { authenticated, email, groups, privilegedGroups, keycloakToken } = authContext;
  const [configData, setConfigData] = useState<ConfigData | null>(null);
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
    fetchConfig().then((config: ConfigType) => {
      setConfigData(config);
    }).catch(error => {
      setError('fetchConfig', error.message);
    });
  }, [setError]);

  /**
   * Builds the Argo CD URL for the current task based on the configuration response.
   *
   * @returns The fully qualified Argo CD link or an empty string when unavailable.
   */
  const getArgoCDUrl = () => {
    if (configData?.argo_cd_url_alias) {
      return `${configData.argo_cd_url_alias}/applications/${task?.app}`;
    } else if (configData?.argo_cd_url) {
      return `${configData.argo_cd_url.Scheme}://${configData.argo_cd_url.Host}${configData.argo_cd_url.Path}/applications/${task?.app}`;
    }
    return '';
  };

  useEffect(() => {
    if (!id) {
      setError('fetchTask', 'Task identifier is not present in the route.');
      return;
    }

    fetchTask(id)
      .then(item => {
        if (!isTask(item)) {
          throw new Error('Unexpected task payload received from the server.');
        }
        setSuccess('fetchTask', 'Fetched task successfully');
        setTask(item);
      })
      .catch((error: Error) => {
        setError('fetchTasks', error.message);
      });
  }, [id, setError, setSuccess]);

  useEffect(() => {
    let intervalId: NodeJS.Timeout;

    const fetchTaskStatus = async () => {
      if (!id) {
        return;
      }
      console.log('Fetching task status...');
      try {
        const updatedTask = await fetchTask(id);
        if (!isTask(updatedTask)) {
          throw new Error('Unexpected task payload received from the server.');
        }
        setTask(updatedTask);
      } catch (error: any) {
        setError('fetchTaskStatus', error.message);
      }
    };

    if (id && task && task.status === 'in progress') {
      intervalId = setInterval(fetchTaskStatus, 10000);
    }

    return () => {
      if (intervalId) {
        clearInterval(intervalId);
      }
    };
  }, [task, id, setError]);

  const userIsPrivileged = groups && privilegedGroups && groups.some((group: string) => privilegedGroups.includes(group));

  /**
   * Requests the backend to redeploy the selected task version under the current user.
   *
   * @throws When the backend rejects the request or returns an unexpected status.
   */
  const rollbackToVersion = async () => {
    if (!task) {
      return;
    }
    if (!email) {
      setError('fetchTasks', 'Unable to rollback without a known author.');
      return;
    }

    const updatedTask = {
      ...task,
      author: email,
    };

    const apiEndpoint = (typeof window !== 'undefined' && window.location)
      ? `${window.location.origin}/api/v1/tasks`
      : '/api/v1/tasks';

    try {
      const response = await fetch(apiEndpoint, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Keycloak-Authorization': keycloakToken || '',
        },
        body: JSON.stringify(updatedTask),
      });

      if (response.status === 401) {
        throw new Error('You are not authorized to perform this action!');
      } else if (response.status === 406) {
        throw new Error('Lockdown is active. Deployments are forbidden!');
      } else if (response.status !== 202) {
        throw new Error(`Received unexpected status code: ${response.status}`);
      }

      navigate('/');
    } catch (error: any) {
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
                {formatDateTime(Number(task.created))}
              </Typography>
            </Grid>
            <Grid item xs={12} sm={6}>
              <Typography variant="body2" color="textSecondary">
                Updated
              </Typography>
              <Typography variant="body1">
                {formatDateTime(Number(task.updated))}
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
