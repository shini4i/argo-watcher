import Box from '@mui/material/Box';
import AppBar from '@mui/material/AppBar';
import Toolbar from '@mui/material/Toolbar';
import Typography from '@mui/material/Typography';
import GitHubIcon from '@mui/icons-material/GitHub';
import Link from '@mui/material/Link';
import { Link as RouterLink } from 'react-router-dom';
import Stack from '@mui/material/Stack';
import { fetchVersion } from '../Services/Data';
import { useEffect, useState } from 'react';
import LocalOfferIcon from '@mui/icons-material/LocalOffer';
import RestoreIcon from '@mui/icons-material/Restore';
import CalendarMonthIcon from '@mui/icons-material/CalendarMonth';
import Tooltip from '@mui/material/Tooltip';
import { useErrorContext } from '../ErrorContext';

function NavigationButton({ to, children }) {
  return (
    <Link
      sx={{ color: 'white', mx: '10px', display: 'flex' }}
      component={RouterLink}
      to={to}
    >
      {children}
    </Link>
  );
}

function Navbar() {
  const [version, setVersion] = useState('0.0.0');
  const { setSuccess, setError } = useErrorContext();
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
          <Typography variant="h6" component="div" sx={{ minWidth: '120px' }}>
            Argo Watcher
          </Typography>
          <Stack
            sx={{ flexGrow: 1, px: 2 }}
            spacing={{ xs: 3, md: 1 }}
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
            spacing={1}
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
            component={Link}
            href={'https://github.com/shini4i/argo-watcher/tree/v' + version}
          >
            <GitHubIcon sx={{ fontSize: '1.7em' }} />
            <Stack>
              <Typography fontSize={'14px'}>GitHub</Typography>
              <Typography fontSize={'11px'}>
                <LocalOfferIcon fontSize={'6px'} /> {version}
              </Typography>
            </Stack>
          </Stack>
        </Toolbar>
      </AppBar>
    </Box>
  );
}

export default Navbar;
