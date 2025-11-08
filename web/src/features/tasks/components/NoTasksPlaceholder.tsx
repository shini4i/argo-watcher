import { useEffect } from 'react';
import { Box, CircularProgress, Stack, Typography } from '@mui/material';
import { useListContext } from 'react-admin';
import type { RaRecord } from 'react-admin';

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
  const { refetch } = useListContext<RaRecord>();

  useEffect(() => {
    if (!refetch) {
      return undefined;
    }

    const browserWindow = globalThis.window;
    if (!browserWindow) {
      return undefined;
    }

    const id = browserWindow.setInterval(() => {
      refetch().catch((error) => {
        if (import.meta.env.DEV) {
          console.warn('NoTasksPlaceholder refetch failed', error);
        }
      });
    }, reloadIntervalMs);

    return () => browserWindow.clearInterval(id);
  }, [refetch, reloadIntervalMs]);

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
          Auto-refreshing every {Math.round(reloadIntervalMs / 1000)} seconds…
        </Typography>
      </Stack>
    </Box>
  );
};
