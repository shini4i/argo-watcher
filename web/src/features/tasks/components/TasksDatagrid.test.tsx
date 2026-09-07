import { fireEvent, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import { createTheme, ThemeProvider } from '@mui/material/styles';
import type { Task } from '../../../data/types';
import { AUTHOR_MAX_WIDTH, TasksDatagrid, __testing } from './TasksDatagrid';
import { TaskListProvider, useTaskListContext } from './TaskListContext';
import { tokens } from '../../../theme/tokens';

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
  useListContext: () => ({ filterValues: {} }),
  useNotify: () => vi.fn(),
}));

const renderInRouter = (ui: ReactNode) =>
  render(<MemoryRouter>{ui}</MemoryRouter>);

describe('TasksDatagrid', () => {
  it('opens the task when a row is clicked instead of expanding it', () => {
    datagridPropsLog.length = 0;
    renderInRouter(<TasksDatagrid />);

    const props = datagridPropsLog.at(-1);
    expect(props).toMatchObject({ bulkActionButtons: false });
    expect(props?.expand).toBeUndefined();
    expect(props?.isRowExpandable).toBeUndefined();

    const rowClick = props?.rowClick as (id: string) => string;
    expect(rowClick('task-1')).toBe('/task/task-1');
  });

  it('escapes an id that would otherwise break out of the path', () => {
    const { rowClickToTask } = __testing;
    expect(rowClickToTask('a/b?c')).toBe('/task/a%2Fb%3Fc');
  });

  it('renders the six task columns and no details action', () => {
    renderInRouter(<TasksDatagrid />);
    expect(screen.getByTestId('function-app')).toBeInTheDocument();
    expect(screen.getByTestId('function-status')).toBeInTheDocument();
    expect(screen.getByTestId('function-images')).toBeInTheDocument();
    expect(screen.getByTestId('function-author')).toHaveTextContent(sampleRecord.author);
    expect(screen.getByTestId('function-created')).toBeInTheDocument();
    expect(screen.getByTestId('function-duration')).toBeInTheDocument();
    expect(screen.queryByTestId('function-Details')).toBeNull();
  });

  it('drops the separate project column — the app cell carries the project', () => {
    renderInRouter(<TasksDatagrid />);
    expect(screen.queryByTestId('function-project')).toBeNull();
    expect(screen.getByTestId('function-app')).toHaveTextContent('github.com/repo');
  });

  it('collapses created and updated into one column', () => {
    renderInRouter(<TasksDatagrid />);
    expect(screen.queryByTestId('function-updated')).toBeNull();
  });

  it('moves the rollback flag out of the status cell and onto the app', () => {
    renderInRouter(<TasksDatagrid />);
    expect(screen.getByTestId('function-status')).not.toHaveTextContent('Rollback');
    expect(screen.getByTestId('function-app')).toHaveTextContent('Rollback');
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

  it('adds no pause reason for the failure panel — it is static', () => {
    const Probe = () => {
      const ctx = useTaskListContext();
      return <span data-testid="reasons">{Array.from(ctx.state.pausedReasons).join(',')}</span>;
    };
    const { TaskRow } = __testing;

    renderInRouter(
      <TaskListProvider>
        <Probe />
        <table>
          <TaskRow></TaskRow>
        </table>
      </TaskListProvider>,
    );

    expect(screen.getByTestId('reasons').textContent).toBe('');
  });

  describe('TaskRow', () => {
    const { TaskRow } = __testing;

    it('renders the failure panel beneath a row carrying a status reason', () => {
      renderInRouter(
        <table>
          <TaskRow></TaskRow>
        </table>,
      );

      expect(screen.getByTestId('datagrid-row')).toBeInTheDocument();
      expect(screen.getByText('all green')).toBeInTheDocument();
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
});

describe('ThemeProvider smoke', () => {
  it('renders the datagrid under a dark theme without throwing', () => {
    render(
      <MemoryRouter>
        <ThemeProvider theme={createTheme({ palette: { mode: 'dark' } })}>
          <TasksDatagrid />
        </ThemeProvider>
      </MemoryRouter>,
    );
    expect(screen.getByTestId('datagrid')).toBeInTheDocument();
  });
});
