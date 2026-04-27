import { type ReactNode } from 'react';
import { Box, Button, Stack, Typography } from '@mui/material';
import InboxOutlinedIcon from '@mui/icons-material/InboxOutlined';
import FilterAltOutlinedIcon from '@mui/icons-material/FilterAltOutlined';

type EmptyStateIcon = 'inbox' | 'filter';

interface EmptyStateProps {
  readonly icon?: EmptyStateIcon;
  readonly title: string;
  readonly description?: string;
  readonly cta?: ReactNode;
}

const ICONS: Record<EmptyStateIcon, ReactNode> = {
  inbox: <InboxOutlinedIcon sx={{ fontSize: 36, color: 'text.disabled' }} />,
  filter: <FilterAltOutlinedIcon sx={{ fontSize: 36, color: 'text.disabled' }} />,
};

/**
 * Static empty-state placeholder shown when the task list returns zero rows.
 * No spinner — initial-load skeletons are owned by react-admin's <Datagrid>.
 */
export const EmptyState = ({ icon = 'inbox', title, description, cta }: EmptyStateProps) => (
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
    <Stack spacing={1.5} alignItems="center" sx={{ maxWidth: 420 }}>
      {ICONS[icon]}
      <Typography variant="h6">{title}</Typography>
      {description && (
        <Typography variant="body2" color="text.secondary">
          {description}
        </Typography>
      )}
      {cta}
    </Stack>
  </Box>
);

interface EmptyStateCtaProps {
  readonly label: string;
  readonly onClick: () => void;
}

/** Convenience CTA button that matches the design's "Clear filters" affordance. */
export const EmptyStateCta = ({ label, onClick }: EmptyStateCtaProps) => (
  <Button variant="outlined" size="small" onClick={onClick} sx={{ mt: 1 }}>
    {label}
  </Button>
);
