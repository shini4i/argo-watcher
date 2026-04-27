import { Box, CircularProgress } from '@mui/material';

/**
 * Standalone spinner placeholder. Reserved for cases where react-admin's
 * built-in skeleton rows aren't appropriate (e.g. ad-hoc loaders outside of a
 * <List>). The list pages themselves rely on react-admin's skeleton.
 */
export const LoadingState = () => (
  <Box
    role="status"
    aria-label="Loading"
    sx={{
      minHeight: 320,
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
    }}
  >
    <CircularProgress size={32} thickness={4} />
  </Box>
);
