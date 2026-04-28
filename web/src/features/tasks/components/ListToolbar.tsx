import { type ReactNode } from 'react';
import { Stack } from '@mui/material';

interface ListToolbarProps {
  readonly left?: ReactNode;
  readonly right?: ReactNode;
}

/**
 * Layout shell for the toolbar row above the task tables.
 * Left slot is page-specific (status tabs or date-range picker); right slot
 * holds search + refresh controls (Recent only).
 */
export const ListToolbar = ({ left, right }: ListToolbarProps) => (
  <Stack
    direction={{ xs: 'column', md: 'row' }}
    spacing={{ xs: 1.5, md: 2 }}
    alignItems={{ xs: 'stretch', md: 'center' }}
    justifyContent="space-between"
    sx={{ width: '100%', minHeight: 48, py: 0.5 }}
  >
    <Stack
      direction="row"
      spacing={1}
      alignItems="center"
      sx={{ flexShrink: 0 }}
    >
      {left}
    </Stack>
    <Stack
      direction="row"
      spacing={1}
      alignItems="center"
      sx={{ flexShrink: 0 }}
    >
      {right}
    </Stack>
  </Stack>
);
