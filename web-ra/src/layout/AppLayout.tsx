import type { LayoutProps } from 'react-admin';
import { Layout } from 'react-admin';
import type { SxProps, Theme } from '@mui/material/styles';
import { AppTopBar } from './components/AppTopBar';

/**
 * Custom application layout ensuring the branded AppBar sits flush with the viewport while
 * delegating the rest of the shell concerns to React-admin.
 */
const layoutSx: SxProps<Theme> = theme => ({
  '& .RaLayout-appFrame': {
    marginTop: 0,
    [theme.breakpoints.down('sm')]: {
      marginTop: 0,
    },
  },
});

export const AppLayout = (props: LayoutProps) => (
  <Layout
    {...props}
    appBar={AppTopBar}
    sx={layoutSx}
  />
);
