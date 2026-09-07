import { Children, isValidElement, useCallback, useEffect, type ReactElement } from 'react';
import { Box, Typography } from '@mui/material';
import { type SxProps, type Theme } from '@mui/material/styles';
import {
  Datagrid,
  DatagridBody,
  DatagridRow,
  FunctionField,
  useListContext,
  useRecordContext,
} from 'react-admin';
import type { Task } from '../../../data/types';
import { tokens } from '../../../theme/tokens';
import { AppCell } from './AppCell';
import { DurationField } from './DurationField';
import { EmptyCell } from './EmptyCell';
import { EmptyState, EmptyStateCta } from './EmptyState';
import { ImagesCell } from './ImagesCell';
import { StatusPill } from './StatusPill';
import { TaskFailureRow } from './TaskFailureRow';
import { TimeCell } from './TimeCell';
import { useTaskListContext } from './TaskListContext';
import { summariseFailure } from '../utils/failureReason';
import { isFailedStatus, isRunningStatus } from '../utils/statusPresentation';

/** Widest the author text may grow, in px, before the address is ellipsised. */
export const AUTHOR_MAX_WIDTH = 180;

/** Navigating a whole row means nested links and buttons must stopPropagation. */
const rowClickToTask = (id: string | number) => `/task/${encodeURIComponent(String(id))}`;

/**
 * @description The task table, shared by the recent and history views. A click
 * anywhere in a row opens that task; a row carrying a status reason is followed
 * by an always-open panel showing it. The wrapping div emits `pause('hover')`
 * so the toolbar's countdown freezes while the cursor is over the table body.
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
      rowClick={rowClickToTask}
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
        render={(record: Task) => (
          <AppCell app={record.app} project={record.project} isRollback={record.is_rollback} />
        )}
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
        source="images"
        label="Image · tag"
        sortable={false}
        cellClassName="cell-images"
        headerClassName="cell-images"
        render={(record: Task) => <ImagesCell images={record.images} />}
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
        source="created"
        label="When"
        sortBy="created"
        cellClassName="cell-when"
        headerClassName="cell-when"
        render={(record: Task) => <TimeCell ts={record.created} />}
      />
      <FunctionField
        source="duration"
        label="Duration"
        sortable={false}
        cellClassName="cell-duration"
        headerClassName="cell-duration"
        render={(record: Task) => <DurationField record={record} />}
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
  const summary = summariseFailure(record?.status_reason);
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
    // Fixed layout, so the declared column widths hold instead of the browser
    // redistributing slack into every column and bloating them (issue seen
    // after the column set shrank from ten to six). Image · tag declares no
    // width and absorbs whatever is left over.
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
      borderBottom: `1px solid ${theme.palette.divider}`,
    },
    // Each row draws the divider ABOVE itself, not below. A reason panel is not
    // a .RaDatagrid-row, so no line is drawn between a row and its own panel,
    // while the next task's own top border still closes the block off.
    '& .RaDatagrid-row': {
      borderTop: `1px solid ${theme.palette.divider}`,
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
    '& .cell-app': { width: 290 },
    '& .cell-status': { width: 150 },
    // No width: this is the column that takes up the remaining space.
    '& .cell-images': { minWidth: 200 },
    '& .cell-author': { width: AUTHOR_MAX_WIDTH },
    '& .cell-when': {
      width: 150,
      textAlign: 'right',
      fontVariantNumeric: 'tabular-nums',
    },
    '& .cell-duration': {
      width: 100,
      textAlign: 'right',
      fontVariantNumeric: 'tabular-nums',
    },
    // MUI left-aligns a header cell regardless of its column's own alignment.
    '& .RaDatagrid-headerCell.cell-when, & .RaDatagrid-headerCell.cell-duration': {
      textAlign: 'right',
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

/**
 * The "Clear filters" CTA drains all three sinks (URL, storage, react-admin
 * filterValues) via the page's registered clearAll handler — react-admin's
 * default ListNoResults only resets filterValues, leaving the toolbar chips
 * stuck.
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
  TaskRow,
  datagridSx,
  rowClickToTask,
  taskRowSx,
};
