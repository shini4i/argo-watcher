import { fireEvent, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import { createTheme } from '@mui/material/styles';
import type { Task } from '../../../data/types';
import { tokens } from '../../../theme/tokens';
import { AUTHOR_MAX_WIDTH, TasksDatagrid, __testing } from './TasksDatagrid';
import { TaskListProvider, useTaskListContext } from './TaskListContext';

const sampleRecord: Task = {
  id: 'task-1',
  app: 'demo',
  author: 'alice',
  created: 1,
  updated: 2,
  project: 'https://github.com/org/repo/',
  images: [
    { image: 'app', tag: '1' },
    { image: 'worker', tag: '2' },
  ],
  status: 'deployed',
  status_reason: 'all green',
  is_rollback: true,
};

const datagridPropsLog: Array<Record<string, unknown>> = [];

/** Mutable so a test can drive FilteredEmptyState down either branch. */
const listContext: { filterValues: Record<string, unknown> } = { filterValues: {} };

vi.mock('react-admin', () => ({
  Datagrid: (props: Record<string, unknown>) => {
    datagridPropsLog.push(props);
    return <div data-testid="datagrid">{props.children as ReactNode}</div>;
  },
  DatagridBody: (props: Record<string, unknown>) => <tbody data-testid="datagrid-body">{props.row as ReactNode}</tbody>,
  DatagridRow: (props: Record<string, unknown>) => <tr data-testid="datagrid-row">{props.children as ReactNode}</tr>,
  FunctionField: ({ label, render, source }: { label?: string; source?: string; render: (record: Task) => ReactNode }) => (
    <div data-testid={`function-${source ?? label}`}>{render(sampleRecord)}</div>
  ),
  useRecordContext: () => sampleRecord,
  useListContext: () => listContext,
  // The failure panel's Copy button reaches for it through useCopyToClipboard.
  useNotify: () => vi.fn(),
}));

const renderInRouter = (ui: ReactNode) =>
  render(<MemoryRouter>{ui}</MemoryRouter>);

describe('TasksDatagrid', () => {
  it('leaves the row inert — the View button is the only way into a task', () => {
    datagridPropsLog.length = 0;
    renderInRouter(<TasksDatagrid />);

    const props = datagridPropsLog.at(-1);
    expect(props).toMatchObject({ bulkActionButtons: false, rowClick: false });
    // The reason panel goes through DatagridBody, not the expand mechanism.
    expect(props?.expand).toBeUndefined();
    expect(props?.isRowExpandable).toBeUndefined();
    expect(props?.body).toBeDefined();
  });

  it('renders all expected task columns in the new layout', () => {
    renderInRouter(<TasksDatagrid />);
    expect(screen.getByTestId('function-app')).toBeInTheDocument();
    expect(screen.getByTestId('function-project')).toBeInTheDocument();
    expect(screen.getByTestId('function-author')).toHaveTextContent(sampleRecord.author);
    expect(screen.getByTestId('function-status')).toBeInTheDocument();
    expect(screen.getByTestId('function-created')).toBeInTheDocument();
    expect(screen.getByTestId('function-updated')).toBeInTheDocument();
    expect(screen.getByTestId('function-duration')).toBeInTheDocument();
    expect(screen.getByTestId('function-images')).toBeInTheDocument();
    expect(screen.getByTestId('function-Details')).toBeInTheDocument();
  });

  it('renders the failure panel beneath a row carrying a status reason', () => {
    const { TaskRow } = __testing;
    renderInRouter(
      <table>
        <tbody>
          <TaskRow></TaskRow>
        </tbody>
      </table>,
    );

    expect(screen.getByTestId('datagrid-row')).toBeInTheDocument();
    expect(screen.getByText(sampleRecord.status_reason!)).toBeInTheDocument();
  });

  it('pauses auto-refresh while the cursor is over the table body', () => {
    const Probe = () => {
      const ctx = useTaskListContext();
      return <span data-testid="reasons">{Array.from(ctx.state.pausedReasons).join(',')}</span>;
    };
    renderInRouter(
      <TaskListProvider>
        <Probe />
        <TasksDatagrid />
      </TaskListProvider>,
    );

    const wrapper = screen.getByTestId('datagrid').parentElement!;
    expect(screen.getByTestId('reasons').textContent).toBe('');

    fireEvent.mouseEnter(wrapper);
    expect(screen.getByTestId('reasons').textContent).toBe('hover');

    fireEvent.mouseLeave(wrapper);
    expect(screen.getByTestId('reasons').textContent).toBe('');
  });

  it('adds no pause reason for the failure panel — it is always open, not expanded', () => {
    const { TaskRow } = __testing;
    const Probe = () => {
      const ctx = useTaskListContext();
      return <span data-testid="reasons">{Array.from(ctx.state.pausedReasons).join(',')}</span>;
    };

    renderInRouter(
      <TaskListProvider>
        <Probe />
        <table>
          <tbody>
            <TaskRow></TaskRow>
          </tbody>
        </table>
      </TaskListProvider>,
    );

    expect(screen.getByTestId('reasons').textContent).toBe('');
  });

  it('flags a rollback in the application cell, not the status cell', () => {
    renderInRouter(<TasksDatagrid />);
    // The chip qualifies the deployment, so it travels with the app name.
    expect(screen.getByTestId('function-app')).toHaveTextContent('Rollback');
    expect(screen.getByTestId('function-status')).not.toHaveTextContent('Rollback');
  });

  describe('datagridSx', () => {
    const { datagridSx } = __testing;
    const resolve = (mode: 'light' | 'dark') => {
      const theme = createTheme({ palette: { mode } });
      const factory = datagridSx as (theme: unknown) => Record<string, Record<string, unknown>>;
      return { sx: factory(theme), divider: theme.palette.divider };
    };

    it('paints the header rule as an inset shadow, never as a collapsed border', () => {
      const { sx, divider } = resolve('dark');
      const header = sx['& .RaDatagrid-headerCell'];
      // A collapsed border belongs to the table, not the cell, so a sticky
      // header cannot carry it and the rule renders in fragments once stuck.
      expect(header.position).toBe('sticky');
      expect(header.borderBottom).toBe('none');
      expect(header.boxShadow).toBe(`inset 0 -1px 0 ${divider}`);
    });

    it('scopes the row rule to the body, which the header row is not part of', () => {
      const { sx } = resolve('light');
      // react-admin puts RaDatagrid-row on the header row too, so an unscoped
      // rule hands the header the task rows' hover tint.
      expect(sx['& .RaDatagrid-row']).toBeUndefined();
      expect(sx['& tbody .RaDatagrid-row']).toBeDefined();
    });

    it('drops the first row top border so it does not stack with the shadow', () => {
      const row = resolve('light').sx['& tbody .RaDatagrid-row'];
      expect(row['&:first-of-type']).toEqual({ borderTop: 'none' });
    });

    it('tints the row hover per theme', () => {
      const hover = (mode: 'light' | 'dark') =>
        (resolve(mode).sx['& tbody .RaDatagrid-row']['&:hover'] as Record<string, string>)
          .backgroundColor;
      expect(hover('light')).toBe(tokens.rowHoverLight);
      expect(hover('dark')).toBe(tokens.rowHoverDark);
    });
  });

  describe('taskRowSx', () => {
    const { taskRowSx } = __testing;
    const resolve = (record: Task) => {
      const factory = taskRowSx(record) as (theme: unknown) => Record<string, string>;
      return factory(createTheme({ palette: { mode: 'light' } }));
    };

    it('marks a failed row with the failed edge and tint', () => {
      const style = resolve({ ...sampleRecord, status: 'failed' });
      expect(style.borderLeft).toBe(`4px solid ${tokens.statusFailedFg}`);
      expect(style.backgroundColor).toBe(tokens.rowFailedBg);
    });

    it('treats an aborted task as failed — the deployment did not land', () => {
      expect(resolve({ ...sampleRecord, status: 'aborted' }).borderLeft).toBe(
        `4px solid ${tokens.statusFailedFg}`,
      );
    });

    it('marks a running row with the running edge and tint', () => {
      const style = resolve({ ...sampleRecord, status: 'in progress' });
      expect(style.borderLeft).toBe(`4px solid ${tokens.statusRunningFg}`);
      expect(style.backgroundColor).toBe(tokens.rowRunningBg);
    });

    it('keeps a transparent edge on a settled row so widths never shift', () => {
      expect(resolve({ ...sampleRecord, status: 'deployed' }).borderLeft).toBe(
        '4px solid transparent',
      );
    });
  });

  describe('FilteredEmptyState', () => {
    const { FilteredEmptyState } = __testing;

    it('says nothing matches the view when no filter is active', () => {
      listContext.filterValues = {};
      renderInRouter(
        <TaskListProvider>
          <FilteredEmptyState />
        </TaskListProvider>,
      );

      expect(screen.getByText('No tasks to show')).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: 'Clear filters' })).toBeNull();
    });

    it('offers the clear-filters CTA once a filter is narrowing the list', () => {
      listContext.filterValues = { status: 'failed' };
      renderInRouter(
        <TaskListProvider>
          <FilteredEmptyState />
        </TaskListProvider>,
      );

      expect(screen.getByText('No tasks match the active filters')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Clear filters' })).toBeInTheDocument();
      listContext.filterValues = {};
    });
  });

  describe('AuthorCell', () => {
    const { AuthorCell } = __testing;

    it('renders an em-dash placeholder when the author is empty', () => {
      render(<AuthorCell author="" />);
      expect(screen.getByText('—')).toBeInTheDocument();
    });

    it('caps a long author at a fixed width and keeps the full value in the tooltip', () => {
      const author = 'project_1758_bot_062d75c8b91e27fa4e5bb374cd9c1c39@noreply.gitlab.dyninno.net';
      render(<AuthorCell author={author} />);

      const cell = screen.getByText(author);
      expect(cell).toHaveStyle({
        // Without an explicit block box the unbreakable address sets the cell's
        // min-content width and stretches the whole table.
        display: 'block',
        maxWidth: `${AUTHOR_MAX_WIDTH}px`,
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
      });
      expect(cell).toHaveAttribute('title', author);
    });
  });

  describe('ProjectCell', () => {
    const { ProjectCell } = __testing;

    it('renders an em-dash placeholder when project is empty', () => {
      render(<ProjectCell project={null} />);
      expect(screen.getByText('—')).toBeInTheDocument();
    });

    it('renders plain projects as monospace text', () => {
      render(<ProjectCell project="infra/prod" />);
      expect(screen.getByText('infra/prod')).toBeInTheDocument();
      expect(screen.queryByRole('link')).toBeNull();
    });

    it('renders URL projects as external links', () => {
      render(<ProjectCell project="https://github.com/org/repo/" />);
      const link = screen.getByRole('link');
      expect(link).toHaveAttribute('href', 'https://github.com/org/repo/');
      expect(link).toHaveAttribute('target', '_blank');
      expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'));
    });
  });

  describe('ViewButton', () => {
    const { ViewButton } = __testing;

    it('navigates to the task detail page', () => {
      renderInRouter(<ViewButton id="task-42" />);
      expect(screen.getByRole('link', { name: /view/i })).toHaveAttribute('href', '/task/task-42');
    });

    it('escapes an id that would otherwise break out of the path', () => {
      renderInRouter(<ViewButton id="a/b?c" />);
      expect(screen.getByRole('link', { name: /view/i })).toHaveAttribute('href', '/task/a%2Fb%3Fc');
    });
  });
});
