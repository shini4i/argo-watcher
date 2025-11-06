import React, { useEffect, useState } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import { AppBar, Box, Toolbar, Typography, Link, Stack, Tooltip } from '@mui/material';
import type { SxProps, Theme } from '@mui/material/styles';
import GitHubIcon from '@mui/icons-material/GitHub';
import RestoreIcon from '@mui/icons-material/Restore';
import CalendarMonthIcon from '@mui/icons-material/CalendarMonth';
import QuizRoundedIcon from '@mui/icons-material/QuizRounded';
import LocalOfferIcon from '@mui/icons-material/LocalOffer';

import { fetchVersion } from '../Services/Data';
import { useErrorContext } from '../ErrorContext';
import Sidebar from './Sidebar';

interface NavigationButtonProps {
  to: string;
  children: React.ReactNode;
  external?: boolean;
}

/**
 * A navigation button component that is used for internal and external links.
 * @component
 * @param {Object} props - The props object for the NavigationButton component.
 * @param {string} props.to - The URL link for the button.
 * @param {boolean} [props.external=false] - Whether the link is an external link.
 * @param {string|ReactNode} props.children - The content of the button.
 * @returns {ReactNode} The rendered NavigationButton component.
 */
const NavigationButton: React.FC<NavigationButtonProps> = ({ to, children, external = false }) => {
  const linkStyles: SxProps<Theme> = theme => ({
    display: 'flex',
    alignItems: 'center',
    color: 'inherit',
    textDecoration: 'none',
    transition: theme.transitions.create('opacity', { duration: theme.transitions.duration.shortest }),
    '&:hover': {
      opacity: 0.8,
    },
  });

  if (external) {
    return (
      <Link
        sx={linkStyles}
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
      sx={linkStyles}
      component={RouterLink}
      to={to}
      underline="none"
    >
      {children}
    </Link>
  );
};

/**
 * Navbar component for the application.
 *
 * @component
 * @returns {JSX.Element} The rendered Navbar component.
 */
const Navbar: React.FC = (): JSX.Element => {
  const [version, setVersion] = useState<string>('0.0.0');
  const [isSidebarOpen, setIsSidebarOpen] = useState<boolean>(false);
  const { setSuccess, setError } = useErrorContext();
  const readTheDocsUrl = 'https://argo-watcher.readthedocs.io';
  const githubProjectUrl = 'https://github.com/shini4i/argo-watcher';

  useEffect(() => {
    fetchVersion()
      .then(version => {
        setSuccess('fetchVersion', 'Fetched current app version');
        setVersion(version);
      })
      .catch(error => {
        if (error instanceof Error) {
          setError('fetchVersion', error.message);
        } else {
          setError('fetchVersion', 'An unknown error occurred');
        }
      });
  }, [setSuccess, setError]);

  return (
    <Box>
      <AppBar position="static" color="primary" enableColorOnDark>
        <Toolbar>
          <Box display="flex" alignItems="center">
            <Box onClick={() => setIsSidebarOpen(true)}>
              <img
                src={process.env.PUBLIC_URL + '/logo.png'}
                alt="Argo Watcher Logo"
                style={{ width: 35, height: 'auto' }}
              />
            </Box>
            <Box ml={1}>
              <Typography fontSize={'15px'}>Argo Watcher</Typography>
            </Box>
          </Box>
          <Stack
            spacing={2}
            sx={{ flexGrow: 1, px: 2 }}
            direction={'row'}
            justifyContent={{ xs: 'center', md: 'flex-start' }}
          >
            <NavigationButton to={'/'}>
              <Tooltip title="Recent">
                <RestoreIcon />
              </Tooltip>
            </NavigationButton>
            <NavigationButton to={'/history'}>
              <Tooltip title="History">
                <CalendarMonthIcon />
              </Tooltip>
            </NavigationButton>
          </Stack>
          <Stack
            spacing={1.5}
            direction={'row'}
            alignItems={'center'}
            justifyContent={'flex-end'}
            sx={theme => ({
              minWidth: '120px',
              color: theme.palette.primary.contrastText,
              '& a': {
                color: 'inherit',
              },
            })}
          >
            {/* Docs Button */}
            <NavigationButton to={`${readTheDocsUrl}/en/v${version}`} external>
              <Tooltip title="Docs">
                <QuizRoundedIcon />
              </Tooltip>
            </NavigationButton>

            {/* GitHub Section */}
            <NavigationButton
              to={`${githubProjectUrl}/tree/v${version}`}
              external
            >
              <Stack spacing={1} direction={'row'} alignItems={'center'}>
                <GitHubIcon style={{ fontSize: '1.7em' }} />
                <Stack>
                  <Typography sx={{ fontSize: '14px' }}>GitHub</Typography>
                  <Typography sx={{ fontSize: '11px' }}>
                    <LocalOfferIcon style={{ fontSize: '10px' }} /> {version}
                  </Typography>
                </Stack>
              </Stack>
            </NavigationButton>
          </Stack>
        </Toolbar>
      </AppBar>
      <Sidebar open={isSidebarOpen} onClose={() => setIsSidebarOpen(false)} />
    </Box>
  );
};

export default Navbar;
