import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Container from '@mui/material/Container';
import Typography from '@mui/material/Typography';
import {useContext, useEffect, useState} from 'react';
import {useNavigate, useParams} from 'react-router-dom';
import {fetchTask} from '../Services/Data';
import {useErrorContext} from '../ErrorContext';
import {
    Checkbox,
    Chip,
    Dialog,
    DialogActions,
    DialogContent,
    DialogContentText,
    DialogTitle,
    Divider,
    FormControlLabel,
    Grid,
    Paper,
    TextField,
} from '@mui/material';
import {chipColorByStatus, formatDateTime, ProjectDisplay, StatusReasonDisplay,} from './TasksTable';
import {AuthContext} from '../auth';
import {fetchConfig} from '../config';

export default function TaskView() {
    const {id} = useParams();
    const [task, setTask] = useState(null);
    const {setError, setSuccess} = useErrorContext();
    const {authenticated, email, groups, privilegedGroups} = useContext(AuthContext);
    const [configData, setConfigData] = useState(null);
    const navigate = useNavigate();
    const [deployToken, setDeployToken] = useState('');
    const [openDeployTokenDialog, setOpenDeployTokenDialog] = useState(false);
    const [showDeployToken, setShowDeployToken] = useState(false);

    useEffect(() => {
        fetchConfig().then(config => {
            setConfigData(config);
        });
    }, []);

    const getArgoCDUrl = () => {
        if (configData && configData.argo_cd_url_alias) {
            return `${configData.argo_cd_url_alias}/applications/${task.app}`;
        } else if (configData && configData.argo_cd_url) {
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
                    'ARGO_WATCHER_DEPLOY_TOKEN': deployToken,
                },
                body: JSON.stringify(updatedTask),
            });

            if (response.status === 401) { // HTTP 401 Unauthorized
                throw new Error("Invalid deploy token!");
            } else if (response.status !== 202) { // HTTP 202 Accepted
                throw new Error(`HTTP error! Status code: ${response.status}`);
            }

            navigate('/');
        } catch (error) {
            setError('fetchTasks', error.message);
        }
    };

    const onDeployTokenChange = (event) => {
        setDeployToken(event.target.value);
    };

    const handleDeployTokenOpen = () => {
        setOpenDeployTokenDialog(true);
    };

    const handleDeployTokenClose = () => {
        setOpenDeployTokenDialog(false);
    };

    const confirmDeployment = async () => {
        if (deployToken.trim() === '') {
            setError('Deploy Token is required!');
            return;
        }
        handleDeployTokenClose();
        await rollbackToVersion();
    };

    const toggleShowDeployToken = () => {
        setShowDeployToken(!showDeployToken);
    };

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
                        {task.status_reason && (
                            <Grid item xs={12} sm={12}>
                                <Typography variant="body2" color="textSecondary">
                                    Status Details
                                </Typography>
                                <Typography variant="body1">
                                    <StatusReasonDisplay reason={task.status_reason}/>
                                </Typography>
                            </Grid>
                        )}
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
                        <Grid item xs={12}>
                            <a href={getArgoCDUrl()} target="_blank" rel="noopener noreferrer">
                                <Button variant="contained" color="primary" style={{marginRight: '10px'}}>
                                    Open in ArgoCD UI
                                </Button>
                            </a>
                            {authenticated && userIsPrivileged && (
                                // Open token input dialog on click
                                <Button variant="contained" color="primary" onClick={handleDeployTokenOpen}>
                                    Rollback to this version
                                </Button>
                            )}
                            <Dialog open={openDeployTokenDialog} onClose={handleDeployTokenClose}>
                                <DialogTitle>Enter Deploy Token</DialogTitle>
                                <DialogContent>
                                    <DialogContentText>
                                        Please enter your deploy token.
                                    </DialogContentText>
                                    <TextField
                                        autoFocus
                                        margin="dense"
                                        label="Deploy Token"
                                        type={showDeployToken ? "text" : "password"}
                                        fullWidth
                                        value={deployToken}
                                        onChange={onDeployTokenChange}
                                    />
                                    <FormControlLabel
                                        control={<Checkbox checked={showDeployToken} onChange={toggleShowDeployToken}/>}
                                        label="Show Deploy Token"
                                    />
                                </DialogContent>
                                <DialogActions>
                                    <Button onClick={handleDeployTokenClose}>Cancel</Button>
                                    <Button onClick={confirmDeployment}>Deploy</Button>
                                </DialogActions>
                            </Dialog>
                        </Grid>
                    </Grid>
                )}
            </Paper>
        </Container>
    );
}
