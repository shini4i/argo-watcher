import { Children, isValidElement, useCallback, useEffect, type ReactElement } from 'react';
import { Box, Button, Link, Typography } from '@mui/material';
import { type SxProps, type Theme } from '@mui/material/styles';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import {
  Datagrid,
  DatagridBody,
  DatagridRow,
  FunctionField,
  useListContext,
  useRecordContext,
} from 'react-admin';
import { Link as RouterLink } from 'react-router-dom';
import type { Task } from '../../../data/types';
import { tokens } from '../../../theme/tokens';
import { AppCell, describeProject } from './AppCell';
import { DurationField } from './DurationField';
import { EmptyCell } from './EmptyCell';
import { EmptyState, EmptyStateCta } from './EmptyState';
import { ImagesCell } from './ImagesCell';
import { StatusPill } from './StatusPill';
import { TaskFailureRow } from './TaskFailureRow';
import { TimeCell } from './TimeCell';
import { useTaskListContext } from './TaskListContext';
import { summariseFailure } from '../utils/failureReason';
import { hasInformativeReason, isFailedStatus, isRunningStatus } from '../utils/statusPresentation';

/** Widest the author text may grow, in px, before the address is ellipsised. */
export const AUTHOR_MAX_WIDTH = 200;

/**
 * @description The task table, shared by the recent and history views. The View
 * button is the only way into a task, and a row carrying a status reason is
 * followed by an always-open panel showing it. The wrapping div emits
 * `pause('hover')` so the toolbar's countdown freezes while the cursor is over
 * the table body.
 */
export const TasksDatagrid = () => {
  const { pause, resume } = useTaskListContext();
  const handleEnter = useCallback(() => pause('hover'), [pause]);
  const handleLeave = useCallback(() => resume('hover'), [resume]);

  // onMouseLeave is not guaranteed to fire if the component unmounts while the
  // cursor is still over the table (e.g. filter change or page leave mid-hover).
  useEffect(() => () => resume('hover'), [resume]);

  return (
    <Box onMouseEnter={handleEnter} onMouseLeave={handleLeave}>
    <Datagrid
      rowClick={false}
      bulkActionButtons={false}
      body={<DatagridBody row={<TaskRow />} />}
      rowSx={taskRowSx}
      empty={<FilteredEmptyState />}
      sx={datagridSx}
    >
      <FunctionField
        source="app"
        label="Application"
        sortBy="app"
        cellClassName="cell-app"
        headerClassName="cell-app"
        render={(record: Task) => <AppCell app={record.app} isRollback={record.is_rollback} />}
      />
      <FunctionField
        source="project"
        label="Project"
        sortBy="project"
        cellClassName="cell-project"
        headerClassName="cell-project"
        render={(record: Task) => <ProjectCell project={record.project} />}
      />
      <FunctionField
        source="author"
        label="Author"
        sortBy="author"
        cellClassName="cell-author"
        headerClassName="cell-author"
        render={(record: Task) => <AuthorCell author={record.author} />}
      />
      <FunctionField
        source="status"
        label="Status"
        sortBy="status"
        cellClassName="cell-status"
        headerClassName="cell-status"
        render={(record: Task) => <StatusPill status={record.status} />}
      />
      <FunctionField
        source="created"
        label="Created"
        sortBy="created"
        cellClassName="cell-created"
        headerClassName="cell-created"
        render={(record: Task) => <TimeCell ts={record.created} mode="date" />}
      />
      <FunctionField
        source="updated"
        label="Updated"
        sortBy="updated"
        cellClassName="cell-updated"
        headerClassName="cell-updated"
        render={(record: Task) => <TimeCell ts={record.updated ?? record.created} mode="relative" />}
      />
      <FunctionField
        source="duration"
        label="Duration"
        sortable={false}
        cellClassName="cell-duration"
        headerClassName="cell-duration"
        render={(record: Task) => <DurationField record={record} />}
      />
      <FunctionField
        source="images"
        label="Images"
        sortable={false}
        cellClassName="cell-images"
        headerClassName="cell-images"
        render={(record: Task) => <ImagesCell images={record.images} />}
      />
      <FunctionField
        label="Details"
        sortable={false}
        cellClassName="cell-view"
        headerClassName="cell-view"
        render={(record: Task) => <ViewButton id={record.id} />}
      />
    </Datagrid>
    </Box>
  );
};

/**
 * @description One task row plus, when the task carries a status reason, the
 * panel row beneath it. DatagridBody clones this per record inside a
 * RecordContextProvider, which is where the record comes from.
 */
const TaskRow = (props: Record<string, unknown>) => {
  const record = useRecordContext<Task>();
  // A cancelled task's reason restates its status, so it earns no panel here.
  const summary = hasInformativeReason(record?.status)
    ? summariseFailure(record?.status_reason)
    : null;
  // The panel spans every data column; the grid renders no checkbox or expander.
  const colSpan = Children.toArray(props.children as ReactElement[]).filter(isValidElement).length;

  return (
    <>
      <DatagridRow {...(props as never)} />
      {record && summary && (
        <TaskFailureRow
          taskId={record.id}
          summary={summary}
          colSpan={colSpan}
          tone={isFailedStatus(record.status) ? 'error' : 'neutral'}
        />
      )}
    </>
  );
};

/** Marks a row that needs attention with a 4px edge in its status colour. */
const taskRowSx = (record: Task): SxProps<Theme> => theme => {
  const isDark = theme.palette.mode === 'dark';
  // Cells carry no bottom rule of their own; the divider lives on the row's top
  // edge, so a row and its reason panel read as one block.
  const flushCells = { '& .MuiTableCell-root': { borderBottom: 'none' } };

  if (isFailedStatus(record.status)) {
    return {
      ...flushCells,
      borderLeft: `4px solid ${isDark ? tokens.statusFailedFgDark : tokens.statusFailedFg}`,
      backgroundColor: isDark ? tokens.rowFailedBgDark : tokens.rowFailedBg,
    };
  }
  if (isRunningStatus(record.status)) {
    return {
      ...flushCells,
      borderLeft: `4px solid ${isDark ? tokens.statusRunningFgDark : tokens.statusRunningFg}`,
      backgroundColor: isDark ? tokens.rowRunningBgDark : tokens.rowRunningBg,
    };
  }
  return { ...flushCells, borderLeft: '4px solid transparent' };
};

const datagridSx: SxProps<Theme> = theme => {
  const headerBg = theme.palette.mode === 'dark' ? tokens.surface2Dark : tokens.surface2;
  const rowHover = theme.palette.mode === 'dark' ? tokens.rowHoverDark : tokens.rowHoverLight;

  return {
    // Fixed, because the always-open reason panel spans every column and its
    // nowrap headline would otherwise set the table's width under auto layout.
    '& .RaDatagrid-table': {
      tableLayout: 'fixed',
      width: '100%',
    },
    '& .RaDatagrid-headerCell': {
      position: 'sticky',
      top: 0,
      zIndex: 1,
      backgroundColor: headerBg,
      textTransform: 'uppercase',
      fontSize: 11,
      letterSpacing: 0.8,
      color: theme.palette.text.secondary,
      // An inset shadow, not a border — including MUI's own TableCell default:
      // border-collapse hands the border to the table, which a sticky cell
      // cannot carry, so it renders in fragments once the header sticks.
      borderBottom: 'none',
      boxShadow: `inset 0 -1px 0 ${theme.palette.divider}`,
    },
    // Scoped to the body — react-admin puts RaDatagrid-row on the header row too.
    // Each row draws the divider ABOVE itself, not below: a reason panel is not
    // a .RaDatagrid-row, so no line falls between a row and its own panel, while
    // the next task's own top border still closes the block off.
    '& tbody .RaDatagrid-row': {
      borderTop: `1px solid ${theme.palette.divider}`,
      transition: theme.transitions.create('background-color', {
        duration: theme.transitions.duration.shortest,
      }),
      '&:hover': {
        backgroundColor: rowHover,
      },
      // The header rule is a shadow, which does not collapse with this border;
      // both together would stack into a 2px line.
      '&:first-of-type': {
        borderTop: 'none',
      },
    },
    '& .RaDatagrid-cell': {
      paddingTop: theme.spacing(1.25),
      paddingBottom: theme.spacing(1.25),
    },
    // Definite widths, not min/max: a fixed layout ignores both.
    '& .cell-app': { width: 240 },
    '& .cell-project': { width: 220 },
    '& .cell-author': { width: AUTHOR_MAX_WIDTH },
    '& .cell-status': { width: 156 },
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

const AuthorCell = ({ author }: { author?: string | null }) => {
  if (!author) {
    return <EmptyCell />;
  }
  return (
    <Typography
      variant="body2"
      sx={{
        // A bot address such as project_1758_bot_<hash>@noreply.example.net has no
        // break opportunity, so without an explicit capped block box it sets the
        // cell's min-content width and stretches the whole table sideways.
        display: 'block',
        maxWidth: AUTHOR_MAX_WIDTH,
        fontFamily: tokens.fontMono,
        fontSize: 11.5,
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
      }}
      title={author}
    >
      {author}
    </Typography>
  );
};

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

/** The only way into a task from the list: the row itself does not navigate. */
const ViewButton = ({ id }: { id: string }) => (
  <Button
    component={RouterLink}
    to={`/task/${encodeURIComponent(id)}`}
    size="small"
    variant="outlined"
  >
    View
  </Button>
);

/**
 * @description The "Clear filters" CTA drains all three sinks (URL, storage,
 * filterValues) through the page's clearAll handler — react-admin's default
 * ListNoResults resets only filterValues, leaving the toolbar chips stuck.
 */
const FilteredEmptyState = () => {
  const { filterValues } = useListContext();
  const { clearAll } = useTaskListContext();
  const hasFilters = Object.keys(filterValues ?? {}).length > 0;

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

export const __testing = {
  AuthorCell,
  ProjectCell,
  TaskRow,
  ViewButton,
  datagridSx,
  taskRowSx,
};
