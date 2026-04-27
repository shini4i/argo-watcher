import { Typography } from '@mui/material';
import { type SxProps, type Theme } from '@mui/material/styles';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import { Datagrid, FunctionField, useRecordContext } from 'react-admin';
import type { Task } from '../../../data/types';
import { tokens } from '../../../theme/tokens';
import { AppCell } from './AppCell';
import { DurationField } from './DurationField';
import { ImagesCell } from './ImagesCell';
import { StatusPill } from './StatusPill';
import { TimeCell } from './TimeCell';

/**
 * Renders the shared task table used by both recent and history views.
 * Row click navigates to the task detail page; the auto-expand chevron in the
 * first column toggles the inline status-reason panel for rows that have one.
 */
export const TasksDatagrid = () => {
  return (
    <Datagrid
      rowClick={(id: string | number) => `/task/${id}`}
      bulkActionButtons={false}
      expand={<StatusReasonPanel />}
      isRowExpandable={(record?: Task) => Boolean(record?.status_reason)}
      expandSingle
      sx={datagridSx}
    >
      <FunctionField
        source="status"
        label="Status"
        sortBy="status"
        cellClassName="cell-status"
        render={(record: Task) => <StatusPill status={record.status} />}
      />
      <FunctionField
        source="app"
        label="Application"
        sortBy="app"
        cellClassName="cell-app"
        render={(record: Task) => <AppCell app={record.app} project={record.project} />}
      />
      <FunctionField
        source="author"
        label="Author"
        sortBy="author"
        cellClassName="cell-author"
        render={(record: Task) => (
          <Typography
            variant="body2"
            sx={{ fontFamily: tokens.fontMono, fontSize: 11.5 }}
            noWrap
            title={record.author}
          >
            {record.author || '—'}
          </Typography>
        )}
      />
      <FunctionField
        source="created"
        label="Created"
        sortBy="created"
        cellClassName="cell-time"
        render={(record: Task) => <TimeCell ts={record.created} relative={record.updated ?? record.created} />}
      />
      <FunctionField
        source="duration"
        label="Duration"
        sortable={false}
        cellClassName="cell-duration"
        render={(record: Task) => <DurationField record={record} />}
      />
      <FunctionField
        source="images"
        label="Images"
        sortable={false}
        cellClassName="cell-images"
        render={(record: Task) => <ImagesCell images={record.images} />}
      />
      <FunctionField
        source="__nav"
        label=""
        sortable={false}
        cellClassName="cell-nav"
        render={() => <ChevronRightIcon fontSize="small" sx={{ color: 'text.disabled' }} />}
      />
    </Datagrid>
  );
};

const datagridSx: SxProps<Theme> = theme => {
  const headerBg = theme.palette.mode === 'dark' ? tokens.surface2Dark : tokens.surface2;
  const rowHover = theme.palette.mode === 'dark' ? tokens.rowHoverDark : tokens.rowHoverLight;

  return {
    '& .RaDatagrid-headerCell': {
      position: 'sticky',
      top: 0,
      zIndex: 1,
      backgroundColor: headerBg,
      textTransform: 'uppercase',
      fontSize: 11,
      letterSpacing: 0.8,
      color: theme.palette.text.secondary,
      borderBottom: `1px solid ${theme.palette.divider}`,
    },
    '& .RaDatagrid-row': {
      borderBottom: `1px solid ${theme.palette.divider}`,
      cursor: 'pointer',
      transition: theme.transitions.create('background-color', {
        duration: theme.transitions.duration.shortest,
      }),
      '&:hover': {
        backgroundColor: rowHover,
      },
    },
    '& .RaDatagrid-cell': {
      paddingTop: theme.spacing(1.25),
      paddingBottom: theme.spacing(1.25),
    },
    '& .RaDatagrid-expandIcon': {
      transition: theme.transitions.create('transform', {
        duration: theme.transitions.duration.shortest,
      }),
    },
    '& .cell-status': { width: 132 },
    '& .cell-author': { width: 130 },
    '& .cell-time, & .cell-duration': {
      width: 170,
      fontVariantNumeric: 'tabular-nums',
    },
    '& .cell-duration': { width: 90 },
    '& .cell-images': { width: 240 },
    '& .cell-nav': {
      width: 56,
      textAlign: 'right',
      paddingLeft: 0,
      paddingRight: theme.spacing(1.5),
    },
  };
};

/** Expanded row rendering the detailed status reason for a task. */
const StatusReasonPanel = () => {
  const record = useRecordContext<Task>();
  return <StatusReasonContent record={record} />;
};

/**
 * Renders the status reason body independent of React-admin context to simplify testing.
 */
const StatusReasonContent = ({ record }: { record?: Task | null }) => {
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
 * Internal testing helpers used to verify formatted sub-components without rendering Datagrid context.
 */
export const __testing = {
  StatusReasonContent,
  datagridSx,
};
