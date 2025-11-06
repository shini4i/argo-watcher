import type { ReactNode } from 'react';
import { Button, Chip, Typography } from '@mui/material';
import { Datagrid, DateField, FunctionField, NumberField, TextField, useRecordContext } from 'react-admin';
import { Link as RouterLink } from 'react-router-dom';
import type { Task } from '../../../data/types';
import { formatDuration, formatRelativeTime } from '../../../shared/utils';
import { describeTaskStatus } from '../utils/statusPresentation';

/**
 * Renders the shared task table used by both recent and history views.
 */
export const TasksDatagrid = () => (
  <Datagrid
    rowClick="expand"
    bulkActionButtons={false}
    expand={<StatusReasonPanel />}
    isRowExpandable={(record?: Task) => Boolean(record?.status_reason)}
  >
    <TextField source="app" label="Application" />
    <TextField source="project" label="Project" />
    <TextField source="author" label="Author" />
    <FunctionField
      label="Status"
      render={(record: Task) => <TaskStatusChip status={record.status} />}
    />
    <DateField source="created" showTime label="Created" />
    <FunctionField label="Updated" render={(record: Task) => formatRelativeTime(record.updated)} />
    <FunctionField
      label="Duration"
      render={(record: Task) => formatDuration(Math.max(0, (record.updated ?? record.created) - record.created))}
    />
    <NumberField source="images.length" label="Images" />
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

const TaskStatusChip = ({ status }: { status?: string | null }) => {
  const presentation = describeTaskStatus(status);
  return (
    <Chip
      size="small"
      label={presentation.label}
      color={presentation.chipColor}
      icon={presentation.icon as ReactNode}
    />
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
