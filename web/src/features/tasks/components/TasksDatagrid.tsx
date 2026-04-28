import { useCallback, useEffect } from 'react';
import { Box, Button, Link, Typography } from '@mui/material';
import { type SxProps, type Theme } from '@mui/material/styles';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { Datagrid, FunctionField, useListContext, useRecordContext } from 'react-admin';
import { Link as RouterLink } from 'react-router-dom';
import type { Task } from '../../../data/types';
import { tokens } from '../../../theme/tokens';
import { AppCell, describeProject } from './AppCell';
import { DurationField } from './DurationField';
import { EmptyCell } from './EmptyCell';
import { EmptyState, EmptyStateCta } from './EmptyState';
import { ImagesCell } from './ImagesCell';
import { StatusPill } from './StatusPill';
import { TimeCell } from './TimeCell';
import { usePauseRefresh, useTaskListContext } from './TaskListContext';

/**
 * Renders the shared task table used by both recent and history views.
 * The leading expand chevron and a row click both toggle the inline
 * status-reason panel for rows that have one. The trailing "View" button is
 * the explicit affordance for navigating to the task detail page so the row
 * body remains a quiet expander.
 *
 * The wrapping div emits `pause('hover')` reasons through TaskListContext so
 * the toolbar's auto-refresh countdown freezes while the cursor is over the
 * table body.
 */
export const TasksDatagrid = () => {
  const { pause, resume } = useTaskListContext();
  const handleEnter = useCallback(() => pause('hover'), [pause]);
  const handleLeave = useCallback(() => resume('hover'), [resume]);

  // onMouseLeave is not guaranteed to fire if the component unmounts while the
  // cursor is still over the table (e.g. filter change or page leave mid-hover).
  // Always clear the hover pause on unmount so the auto-refresh timer never
  // gets stuck.
  useEffect(() => () => resume('hover'), [resume]);

  return (
    <Box onMouseEnter={handleEnter} onMouseLeave={handleLeave}>
    <Datagrid
      rowClick="expand"
      bulkActionButtons={false}
      expand={<StatusReasonPanel />}
      isRowExpandable={(record?: Task) => Boolean(record?.status_reason)}
      expandSingle
      empty={<FilteredEmptyState />}
      sx={datagridSx}
    >
      <FunctionField
        source="app"
        label="Application"
        sortBy="app"
        cellClassName="cell-app"
        render={(record: Task) => <AppCell app={record.app} />}
      />
      <FunctionField
        source="project"
        label="Project"
        sortBy="project"
        cellClassName="cell-project"
        render={(record: Task) => <ProjectCell project={record.project} />}
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
        source="status"
        label="Status"
        sortBy="status"
        cellClassName="cell-status"
        render={(record: Task) => <StatusPill status={record.status} />}
      />
      <FunctionField
        source="created"
        label="Created"
        sortBy="created"
        cellClassName="cell-created"
        render={(record: Task) => <TimeCell ts={record.created} mode="date" />}
      />
      <FunctionField
        source="updated"
        label="Updated"
        sortBy="updated"
        cellClassName="cell-updated"
        render={(record: Task) => <TimeCell ts={record.updated ?? record.created} mode="relative" />}
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
        label="Details"
        sortable={false}
        cellClassName="cell-view"
        render={(record: Task) => <ViewButton id={record.id} />}
      />
    </Datagrid>
    </Box>
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
    '& .cell-app': { minWidth: 200, maxWidth: 280 },
    '& .cell-project': { minWidth: 180, maxWidth: 280 },
    '& .cell-author': { width: 200 },
    '& .cell-status': { width: 132 },
    '& .cell-created': {
      width: 200,
      fontVariantNumeric: 'tabular-nums',
    },
    '& .cell-updated': {
      width: 130,
      fontVariantNumeric: 'tabular-nums',
    },
    '& .cell-duration': { width: 110, fontVariantNumeric: 'tabular-nums' },
    '& .cell-images': { width: 280 },
    '& .cell-view': {
      width: 96,
      textAlign: 'right',
      paddingLeft: 0,
      paddingRight: theme.spacing(1.5),
    },
  };
};

/**
 * Renders the project field. URLs become external links (clicks stopPropagation
 * so following the link does not also expand the row); plain strings render as
 * muted monospace text and inherit the row's click-to-expand behavior.
 */
const ProjectCell = ({ project }: { project?: string | null }) => {
  if (!project) {
    return <EmptyCell />;
  }
  const info = describeProject(project);
  if (info.isUrl && info.href) {
    return (
      <Link
        href={info.href}
        target="_blank"
        rel="noopener noreferrer"
        underline="hover"
        onClick={event => event.stopPropagation()}
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 0.25,
          fontFamily: tokens.fontMono,
          fontSize: 12,
          color: 'text.secondary',
          maxWidth: '100%',
        }}
      >
        <Box
          component="span"
          sx={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
        >
          {info.label}
        </Box>
        <OpenInNewIcon sx={{ fontSize: 12 }} />
      </Link>
    );
  }
  return (
    <Typography
      variant="body2"
      sx={{ fontFamily: tokens.fontMono, fontSize: 12, color: 'text.secondary' }}
      noWrap
      title={info.label}
    >
      {info.label}
    </Typography>
  );
};

/**
 * Explicit "View" affordance in the trailing Details column. stopPropagation
 * prevents the row-level expand from firing when the button is clicked.
 */
const ViewButton = ({ id }: { id: string }) => (
  <Button
    component={RouterLink}
    to={`/task/${encodeURIComponent(id)}`}
    size="small"
    variant="outlined"
    onClick={event => event.stopPropagation()}
  >
    View
  </Button>
);

/**
 * Empty-state shown by the Datagrid when the loaded page returns zero rows
 * but the user still has filters / search active. Wraps EmptyState with a
 * "Clear filters" CTA that drains all three sinks (URL, storage, react-admin
 * filterValues) via the page's registered clearAll handler — react-admin's
 * default ListNoResults only resets filterValues, leaving the toolbar chips
 * stuck.
 */
const FilteredEmptyState = () => {
  const { filterValues } = useListContext();
  const { state, clearAll } = useTaskListContext();
  const hasFilters =
    Object.keys(filterValues ?? {}).length > 0 || Boolean(state.searchQuery);

  if (!hasFilters) {
    return (
      <EmptyState
        icon="inbox"
        title="No tasks to show"
        description="Nothing matches the current view — try adjusting filters above."
      />
    );
  }

  return (
    <EmptyState
      icon="filter"
      title="No tasks match the active filters"
      description="Adjust the filters above or clear them to see every task again."
      cta={<EmptyStateCta label="Clear filters" onClick={clearAll} />}
    />
  );
};

/** Expanded row rendering the detailed status reason for a task. */
const StatusReasonPanel = () => {
  const record = useRecordContext<Task>();
  // The panel mounts when a row expands and unmounts when it collapses, so a
  // life-cycle bound pause('expand') exactly tracks the expanded state.
  usePauseRefresh('expand');
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
  ProjectCell,
  StatusReasonContent,
  StatusReasonPanel,
  ViewButton,
  datagridSx,
};
