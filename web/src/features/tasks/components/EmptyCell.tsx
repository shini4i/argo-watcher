import { Typography } from '@mui/material';

/**
 * Renders a muted em-dash placeholder for empty table cells so the grid stays
 * visually quiet without dropping alignment.
 */
export const EmptyCell = () => (
  <Typography component="span" variant="body2" sx={{ color: 'text.disabled' }}>
    —
  </Typography>
);
