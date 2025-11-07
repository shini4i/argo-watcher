import RestoreIcon from '@mui/icons-material/Restore';
import CalendarMonthIcon from '@mui/icons-material/CalendarMonth';
import QuizRoundedIcon from '@mui/icons-material/QuizRounded';
import GitHubIcon from '@mui/icons-material/GitHub';
import LocalOfferIcon from '@mui/icons-material/LocalOffer';
import { AppBar, Box, Chip, IconButton, Link, Stack, Toolbar, Tooltip, Typography } from '@mui/material';
import type { SxProps, Theme } from '@mui/material/styles';
import type { AppBarProps } from 'react-admin';
import { useNotify } from 'react-admin';
import { Link as RouterLink, useLocation } from 'react-router-dom';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { httpClient } from '../../data/httpClient';
import { ConfigDrawer } from './ConfigDrawer';
import logoUrl from '../../assets/logo.png';

const DOCS_BASE_URL = 'https://argo-watcher.readthedocs.io';
const GITHUB_REPO_URL = 'https://github.com/shini4i/argo-watcher';

const navigationButtons = [
  { to: '/', icon: <RestoreIcon fontSize="small" />, label: 'Recent' },
  { to: '/history', icon: <CalendarMonthIcon fontSize="small" />, label: 'History' },
];

const appBarStyles: SxProps<Theme> = theme => {
  if (theme.palette.mode === 'dark') {
    return {
      backgroundColor: 'transparent',
      backgroundImage: 'linear-gradient(135deg, #1f2937 0%, #111827 100%)',
      boxShadow: '0px 2px 6px rgba(4, 8, 15, 0.45)',
      color: '#f8fafc',
      transition: 'background-color 0.3s ease, background-image 0.3s ease, box-shadow 0.3s ease, color 0.3s ease',
    };
  }

  return {
    backgroundColor: '#2E3B55',
    backgroundImage: 'none',
    boxShadow: '0px 2px 4px rgba(18, 26, 44, 0.18)',
    color: '#f9fbff',
    transition: 'background-color 0.3s ease, background-image 0.3s ease, box-shadow 0.3s ease, color 0.3s ease',
  };
};

const routeIsActive = (pathname: string, target: string) => {
  if (target === '/') {
    return pathname === target;
  }
  return pathname.startsWith(target);
};

/**
 * Branded application bar providing navigation shortcuts, docs/GitHub links,
 * timezone context, and access to the configuration drawer.
 */
export const AppTopBar = (props: AppBarProps) => {
  const { sx: appBarSxProp, ...appBarProps } = props;
  const composedSx: SxProps<Theme> = appBarSxProp ? [appBarStyles, appBarSxProp] : appBarStyles;
  const [version, setVersion] = useState<string>('—');
  const [drawerOpen, setDrawerOpen] = useState(false);
  const notify = useNotify();
  const location = useLocation();

  const fetchVersion = useCallback(async () => {
    try {
      const response = await httpClient<string>('/api/v1/version');
      if (typeof response.data === 'string' && response.data.trim()) {
        setVersion(response.data.trim());
      } else {
        setVersion('unknown');
      }
    } catch (error) {
      setVersion('unknown');
      const message = error instanceof Error ? error.message : 'Unable to fetch version';
      notify(message, { type: 'warning' });
    }
  }, [notify]);

  useEffect(() => {
    void fetchVersion();
  }, [fetchVersion]);

  const docsUrl = useMemo(() => `${DOCS_BASE_URL}/en/v${version}`, [version]);
  const githubVersionUrl = useMemo(() => `${GITHUB_REPO_URL}/tree/v${version}`, [version]);

  return (
    <>
      <AppBar
        {...appBarProps}
        color="primary"
        enableColorOnDark
        position="sticky"
        elevation={0}
        sx={composedSx}
      >
        <Toolbar sx={{ minHeight: { xs: 56, md: 64 }, px: { xs: 1, md: 2 } }}>
          <Stack
            direction="row"
            alignItems="center"
            spacing={1}
            sx={{ width: '100%', flexWrap: { xs: 'wrap', md: 'nowrap' }, rowGap: { xs: 0.75, md: 0 } }}
          >
            <IconButton
              size="small"
              color="inherit"
              edge="start"
              onClick={() => setDrawerOpen(true)}
              aria-label="Open configuration drawer"
              disableRipple
              sx={{ p: 0.5, cursor: 'pointer' }}
            >
              <Box
                component="img"
                src={logoUrl}
                alt="Argo Watcher Logo"
                sx={{ width: 35, height: 'auto' }}
              />
            </IconButton>
            <Typography
              component="div"
              sx={{
                display: { xs: 'none', sm: 'block' },
                fontSize: 15,
                fontWeight: 500,
              }}
            >
              Argo Watcher
            </Typography>

            <Stack
              direction="row"
              spacing={2}
              alignItems="center"
              sx={{ ml: { xs: 0, md: 2 }, flexGrow: 1, justifyContent: { xs: 'center', md: 'flex-start' } }}
            >
              {navigationButtons.map(button => {
                const active = routeIsActive(location.pathname, button.to);
                return (
                  <Tooltip key={button.to} title={button.label}>
                    <IconButton
                      component={RouterLink}
                      to={button.to}
                      size="small"
                      color={active ? 'secondary' : 'inherit'}
                      aria-label={button.label}
                    >
                      {button.icon}
                    </IconButton>
                  </Tooltip>
                );
              })}
            </Stack>

            <Stack direction="row" spacing={2} alignItems="center" sx={{ flexWrap: 'wrap', rowGap: 0.5 }}>
              <Chip
                size="small"
                color="secondary"
                label="Timezone: UTC"
                sx={{ fontWeight: 500, letterSpacing: 0.3 }}
              />
              <Tooltip title="Documentation">
                <Link
                  component="a"
                  href={docsUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  underline="none"
                  color="inherit"
                  sx={theme => ({
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: theme.spacing(0.5),
                    transition: theme.transitions.create('opacity', {
                      duration: theme.transitions.duration.shortest,
                    }),
                    '&:hover': {
                      opacity: 0.8,
                    },
                  })}
                  aria-label={`Open documentation for version ${version}`}
                >
                  <QuizRoundedIcon fontSize="small" />
                </Link>
              </Tooltip>
              <Tooltip title="GitHub repository">
                <Link
                  component="a"
                  href={githubVersionUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  underline="none"
                  color="inherit"
                  sx={theme => ({
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: theme.spacing(1),
                    transition: theme.transitions.create('opacity', {
                      duration: theme.transitions.duration.shortest,
                    }),
                    '&:hover': {
                      opacity: 0.8,
                    },
                  })}
                  aria-label={`Open GitHub tag ${version}`}
                >
                  <GitHubIcon sx={{ fontSize: '1.7em' }} />
                  <Stack spacing={0.5} alignItems="flex-start">
                    <Typography sx={{ fontSize: 14, lineHeight: 1 }}>
                      GitHub
                    </Typography>
                    <Stack direction="row" alignItems="center" spacing={0.5}>
                      <LocalOfferIcon sx={{ fontSize: 12 }} />
                      <Typography sx={{ fontSize: 11, lineHeight: 1 }}>
                        {version}
                      </Typography>
                    </Stack>
                  </Stack>
                </Link>
              </Tooltip>
            </Stack>
          </Stack>
        </Toolbar>
      </AppBar>
      <ConfigDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} version={version} />
    </>
  );
};
