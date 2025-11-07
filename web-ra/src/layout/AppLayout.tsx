import type { ReactElement } from 'react';
import { Layout, type LayoutProps, type SidebarProps } from 'react-admin';
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

/**
 * Minimal menu placeholder that disables React-admin's default sidebar menu,
 * keeping navigation exclusively within the custom top bar.
 */
const EmptyMenu = (): ReactElement | null => null;

/**
 * Sidebar placeholder that collapses the layout width so content can center
 * beneath the top bar without the default drawer gap.
 */
const EmptySidebar = (_props: SidebarProps): ReactElement | null => null;

export const AppLayout = (props: LayoutProps) => (
  <Layout
    {...props}
    appBar={AppTopBar}
    menu={EmptyMenu}
    sidebar={EmptySidebar}
    sx={layoutSx}
  />
);
