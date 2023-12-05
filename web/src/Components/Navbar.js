import React, {useEffect, useState} from 'react';
import PropTypes from 'prop-types';
import Box from '@mui/material/Box';
import AppBar from '@mui/material/AppBar';
import Toolbar from '@mui/material/Toolbar';
import Typography from '@mui/material/Typography';
import GitHubIcon from '@mui/icons-material/GitHub';
import Link from '@mui/material/Link';
import {Link as RouterLink} from 'react-router-dom';
import Stack from '@mui/material/Stack';
import Tooltip from '@mui/material/Tooltip';
import RestoreIcon from '@mui/icons-material/Restore';
import CalendarMonthIcon from '@mui/icons-material/CalendarMonth';
import QuizRoundedIcon from '@mui/icons-material/QuizRounded';
import LocalOfferIcon from '@mui/icons-material/LocalOffer';
import {fetchVersion} from '../Services/Data';
import {useErrorContext} from '../ErrorContext';
import Sidebar from './Sidebar';

function NavigationButton({to, children, external = false}) {
    if (external) {
        return (
            <Link
                sx={{color: 'white', display: 'flex'}}
                href={to}
                underline="none"
                target="_blank"
                rel="noopener noreferrer"
            >
                {children}
            </Link>
        );
    }

    return (
        <Link
            sx={{color: 'white', display: 'flex'}}
            component={RouterLink}
            to={to}
            underline="none"
        >
            {children}
        </Link>
    );
}

NavigationButton.propTypes = {
    to: PropTypes.oneOfType([PropTypes.string, PropTypes.object]).isRequired,
    children: PropTypes.node.isRequired,
    external: PropTypes.bool
};

function Navbar() {
    const [version, setVersion] = useState('0.0.0');
    const [isSidebarOpen, setSidebarOpen] = useState(false);
    const {setSuccess, setError} = useErrorContext();
    const readTheDocsUrl = 'https://argo-watcher.readthedocs.io';
    const githubProjectUrl = 'https://github.com/shini4i/argo-watcher';

    useEffect(() => {
        fetchVersion()
            .then(version => {
                setSuccess('fetchVersion', 'Fetched current app version');
                setVersion(version);
            })
            .catch(error => {
                setError('fetchVersion', error.message);
            });
    }, []);

    return (
        <Box>
            <AppBar position="static">
                <Toolbar>
                    <Box display="flex" alignItems="center" onClick={() => setSidebarOpen(true)}>
                        <img
                            src={process.env.PUBLIC_URL + '/logo.png'}
                            alt="Argo Watcher Logo"
                            style={{width: 35, height: 'auto'}}
                        />
                        <Box ml={1}>
                            {' '}
                            {/* ml = margin-left: To add some space between logo and text */}
                            <Typography fontSize={'15px'}>Argo Watcher</Typography>
                        </Box>
                    </Box>
                    <Stack
                        spacing={2}
                        sx={{flexGrow: 1, px: 2}}
                        direction={'row'}
                        justifyContent={{xs: 'center', md: 'flex-start'}}
                    >
                        <NavigationButton to={'/'}>
                            <Tooltip title="Recent">
                                <RestoreIcon/>
                            </Tooltip>
                        </NavigationButton>
                        <NavigationButton to={'/history'}>
                            <Tooltip title="History">
                                <CalendarMonthIcon/>
                            </Tooltip>
                        </NavigationButton>
                    </Stack>
                    <Stack
                        spacing={1.5}
                        direction={'row'}
                        alignItems={'center'}
                        justifyContent={'flex-end'}
                        sx={{
                            minWidth: '120px',
                            color: 'white',
                            textTransform: 'unset',
                            textDecoration: 'unset',
                            '&:hover': {
                                color: 'rgba(255,255,255,.85)',
                            },
                        }}
                    >
                        {/* Docs Button */}
                        <NavigationButton to={`${readTheDocsUrl}/en/v${version}`} external>
                            <Tooltip title="Docs">
                                <QuizRoundedIcon/>
                            </Tooltip>
                        </NavigationButton>

                        {/* GitHub Section */}
                        <NavigationButton
                            to={`${githubProjectUrl}/tree/v${version}`}
                            external
                        >
                            <Stack spacing={1} direction={'row'} alignItems={'center'}>
                                <GitHubIcon sx={{fontSize: '1.7em'}}/>
                                <Stack>
                                    <Typography fontSize={'14px'}>GitHub</Typography>
                                    <Typography fontSize={'11px'}>
                                        <LocalOfferIcon fontSize={'6px'}/> {version}
                                    </Typography>
                                </Stack>
                            </Stack>
                        </NavigationButton>
                    </Stack>
                </Toolbar>
            </AppBar>
            <Sidebar open={isSidebarOpen} onClose={() => setSidebarOpen(false)}/>
        </Box>
    );
}

export default Navbar;
