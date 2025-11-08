import type { ReactNode } from 'react';
import { Button, Chip, Link, Stack, Typography } from '@mui/material';
import { alpha, type SxProps, type Theme } from '@mui/material/styles';
import { Datagrid, FunctionField, TextField, useRecordContext } from 'react-admin';
import { Link as RouterLink } from 'react-router-dom';
import type { Task } from '../../../data/types';
import { formatDuration, formatRelativeTime, getBrowserWindow } from '../../../shared/utils';
import { describeTaskStatus } from '../utils/statusPresentation';
import { useEffect, useState } from 'react';
import { useTimezone } from '../../../shared/providers/TimezoneProvider';

/**
 * Renders the shared task table used by both recent and history views.
 */
export const TasksDatagrid = () => {
  const { formatDate } = useTimezone();

  return (
    <Datagrid
      rowClick="expand"
      bulkActionButtons={false}
      expand={<StatusReasonPanel />}
      isRowExpandable={(record?: Task) => Boolean(record?.status_reason)}
      sx={datagridSx}
    >
      <TextField source="app" label="Application" />
      <FunctionField label="Project" render={(record: Task) => <ProjectReference project={record.project} />} />
      <TextField source="author" label="Author" />
      <FunctionField
        label="Status"
        render={(record: Task) => <TaskStatusChip status={record.status} />}
      />
      <FunctionField label="Created" render={(record: Task) => formatDate(record.created)} sortBy="created" />
      <FunctionField label="Updated" render={(record: Task) => formatRelativeTime(record.updated)} />
      <FunctionField
        label="Duration"
        render={(record: Task) => <DurationField record={record} />}
      />
      <FunctionField
        label="Images"
        sortable={false}
        render={(record: Task) => <ImagesList images={record.images} />}
      />
      <FunctionField
        label="Details"
        sortable={false}
        render={(record: Task) => (
          <Button component={RouterLink} to={`/task/${record.id}`} size="small" variant="outlined">
            View
          </Button>
        )}
      />
    </Datagrid>
  );
};

const datagridSx: SxProps<Theme> = theme => ({
  '& .RaDatagrid-headerCell': {
    textTransform: 'uppercase',
    fontSize: 11,
    letterSpacing: 0.8,
    color: theme.palette.text.secondary,
    borderBottom: `1px solid ${theme.palette.divider}`,
  },
  '& .RaDatagrid-row': {
    borderBottom: `1px solid ${theme.palette.divider}`,
    transition: theme.transitions.create('background-color', {
      duration: theme.transitions.duration.shortest,
    }),
    '&:hover': {
      backgroundColor: alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.08 : 0.02),
    },
  },
  '& .RaDatagrid-cell': {
    paddingTop: theme.spacing(1.25),
    paddingBottom: theme.spacing(1.25),
  },
  '& .RaDatagrid-cell:last-of-type': {
    textAlign: 'right',
  },
});

const TaskStatusChip = ({ status }: { status?: string | null }) => {
  const presentation = describeTaskStatus(status);
  return (
    <Chip size="small" label={presentation.label} color={presentation.chipColor} icon={presentation.icon} />
  );
};

/**
 * Expanded row rendering the detailed status reason for a task.
 */
const StatusReasonPanel = () => {
  const record = useRecordContext<Task>();
  if (!record) {
    return null;
  }

  if (!record.status_reason) {
    return (
      <Typography variant="body2" color="text.secondary" sx={{ p: 2 }}>
        No additional status reason provided.
      </Typography>
    );
  }

  return (
    <Typography
      component="pre"
      sx={{ p: 2, fontFamily: theme => theme.typography.fontFamily, whiteSpace: 'pre-wrap' }}
    >
      {record.status_reason}
    </Typography>
  );
};

/**
 * Displays the project field, converting URLs into external links similar to the legacy UI.
 */
const ProjectReference = ({ project }: { project?: string | null }) => {
  if (!project) {
    return <Typography variant="body2">—</Typography>;
  }

  const isUrl = project.startsWith('http://') || project.startsWith('https://');
  if (!isUrl) {
    return <Typography variant="body2">{project}</Typography>;
  }

  const label = project.replace(/^https?:\/\//, '').replace(/\/+$/, '');

  return (
    <Link href={project} target="_blank" rel="noopener noreferrer" underline="hover">
      {label}
    </Link>
  );
};

/**
 * Lists every image reference in `image:tag` format instead of only exposing a count.
 */
const ImagesList = ({ images }: { images: Task['images'] }) => {
  if (!images?.length) {
    return <Typography variant="body2">—</Typography>;
  }

  return (
    <Stack spacing={0.25} sx={{ fontFamily: 'monospace' }}>
      {images.map((image, index) => (
        <Typography key={`${image.image}:${image.tag}:${index}`} variant="body2" component="code">
          {image.image}:{image.tag}
        </Typography>
      ))}
    </Stack>
  );
};

const useNowTicker = (enabled: boolean, intervalMs: number = 10000) => {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!enabled) {
      return undefined;
    }
    const browserWindow = getBrowserWindow();
    if (!browserWindow) {
      return undefined;
    }
    const id = browserWindow.setInterval(() => setNow(Date.now()), intervalMs);
    return () => browserWindow.clearInterval(id);
  }, [enabled, intervalMs]);

  return now;
};

/**
 * Computes a live-updating duration for in-progress tasks to match the legacy UX.
 */
const DurationField = ({ record }: { record: Task }) => {
  const inProgress = record.status === 'in progress' && !record.updated;
  const now = useNowTicker(inProgress);
  const effectiveUpdated = record.updated ?? (inProgress ? Math.floor(now / 1000) : record.created);
  const seconds = Math.max(0, effectiveUpdated - record.created);

  return <Typography variant="body2">{formatDuration(seconds)}</Typography>;
};
