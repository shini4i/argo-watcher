import { Typography } from '@mui/material';

/** An em-dash rather than an empty string, so the grid keeps its alignment. */
export const EmptyCell = () => (
  <Typography component="span" variant="body2" sx={{ color: 'text.disabled' }}>
    —
  </Typography>
);
