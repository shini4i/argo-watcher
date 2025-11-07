import { useEffect } from 'react';
import { Box, CircularProgress, Stack, Typography } from '@mui/material';

interface NoTasksPlaceholderProps {
  title: string;
  description: string;
  reloadIntervalMs?: number;
}

/**
 * Centered placeholder that displays a friendly message while periodically reloading the page.
 */
export const NoTasksPlaceholder = ({
  title,
  description,
  reloadIntervalMs = 15_000,
}: NoTasksPlaceholderProps) => {
  useEffect(() => {
    const id = window.setInterval(() => {
      window.location.reload();
    }, reloadIntervalMs);
    return () => window.clearInterval(id);
  }, [reloadIntervalMs]);

  return (
    <Box
      sx={{
        minHeight: 320,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        textAlign: 'center',
        px: 3,
      }}
    >
      <Stack spacing={2} alignItems="center">
        <CircularProgress size={36} thickness={4} />
        <Typography variant="h5">{title}</Typography>
        <Typography variant="body1" color="text.secondary">
          {description}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          Checking again every {Math.round(reloadIntervalMs / 1000)} seconds…
        </Typography>
      </Stack>
    </Box>
  );
};
