import { Box, Link, Typography } from '@mui/material';
import NavigateNextIcon from '@mui/icons-material/NavigateNext';
import { Link as RouterLink } from 'react-router-dom';
import { tokens } from '../../../../theme/tokens';

interface TaskBreadcrumbProps {
  readonly app?: string | null;
  readonly taskId: string;
}

const SEPARATOR = (
  <NavigateNextIcon aria-hidden sx={{ fontSize: 14, color: 'text.disabled', mx: 0.25 }} />
);

/**
 * @description Trail back to the list, and to the app's own slice of it, so a
 * deep link from CI has a real exit instead of a bare Back button.
 */
export const TaskBreadcrumb = ({ app, taskId }: TaskBreadcrumbProps) => (
  <Box
    aria-label="Breadcrumb"
    component="nav"
    sx={{ display: 'flex', alignItems: 'center', fontSize: 13, minWidth: 0 }}
  >
    <Link component={RouterLink} to="/" underline="hover" sx={{ color: 'text.secondary' }}>
      Recent
    </Link>
    {app && (
      <>
        {SEPARATOR}
        <Link
          component={RouterLink}
          to={`/?app=${encodeURIComponent(app)}`}
          underline="hover"
          sx={{ color: 'text.secondary', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
        >
          {app}
        </Link>
      </>
    )}
    {SEPARATOR}
    <Typography
      component="span"
      aria-current="page"
      sx={{ fontFamily: tokens.fontMono, fontSize: 12.5, color: 'text.primary' }}
    >
      {taskId.slice(0, 8)}
    </Typography>
  </Box>
);
