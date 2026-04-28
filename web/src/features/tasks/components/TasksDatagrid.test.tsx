import { fireEvent, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import type { Task } from '../../../data/types';
import { TasksDatagrid, __testing } from './TasksDatagrid';
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
};

const datagridPropsLog: Array<Record<string, unknown>> = [];

vi.mock('react-admin', () => ({
  Datagrid: (props: Record<string, unknown>) => {
    datagridPropsLog.push(props);
    return <div data-testid="datagrid">{props.children as ReactNode}</div>;
  },
  FunctionField: ({ label, render, source }: { label?: string; source?: string; render: (record: Task) => ReactNode }) => (
    <div data-testid={`function-${source ?? label}`}>{render(sampleRecord)}</div>
  ),
  useRecordContext: () => sampleRecord,
  useListContext: () => ({ filterValues: {} }),
}));

const renderInRouter = (ui: ReactNode) =>
  render(<MemoryRouter>{ui}</MemoryRouter>);

describe('TasksDatagrid', () => {
  it('configures Datagrid to expand on row click and treats rows with status_reason as expandable', () => {
    datagridPropsLog.length = 0;
    renderInRouter(<TasksDatagrid />);

    const props = datagridPropsLog.at(-1);
    expect(props).toMatchObject({ bulkActionButtons: false, expandSingle: true, rowClick: 'expand' });
    expect(typeof props?.isRowExpandable).toBe('function');
    expect((props?.isRowExpandable as (record?: Task) => boolean)({ ...sampleRecord, status_reason: '' })).toBe(false);
    expect((props?.isRowExpandable as (record?: Task) => boolean)(sampleRecord)).toBe(true);
  });

  it('renders all expected task columns in the new layout', () => {
    renderInRouter(<TasksDatagrid />);
    expect(screen.getByTestId('function-app')).toBeInTheDocument();
    expect(screen.getByTestId('function-project')).toBeInTheDocument();
    expect(screen.getByTestId('function-author')).toBeInTheDocument();
    expect(screen.getByTestId('function-status')).toBeInTheDocument();
    expect(screen.getByTestId('function-created')).toBeInTheDocument();
    expect(screen.getByTestId('function-updated')).toBeInTheDocument();
    expect(screen.getByTestId('function-duration')).toBeInTheDocument();
    expect(screen.getByTestId('function-images')).toBeInTheDocument();
    expect(screen.getByTestId('function-__view')).toBeInTheDocument();
  });

  it('renders status reason content based on record data', () => {
    const { StatusReasonContent } = __testing;

    const { rerender } = render(<StatusReasonContent record={undefined} />);
    expect(screen.queryByText(/No additional/)).toBeNull();

    rerender(<StatusReasonContent record={{ ...sampleRecord, status_reason: '' }} />);
    expect(screen.getByText(/No additional status reason/)).toBeInTheDocument();

    rerender(<StatusReasonContent record={sampleRecord} />);
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

  it('pauses auto-refresh while the status-reason panel is mounted', () => {
    const { StatusReasonPanel } = __testing;
    const Probe = () => {
      const ctx = useTaskListContext();
      return <span data-testid="reasons">{Array.from(ctx.state.pausedReasons).join(',')}</span>;
    };

    const { rerender } = render(
      <TaskListProvider>
        <Probe />
      </TaskListProvider>,
    );
    expect(screen.getByTestId('reasons').textContent).toBe('');

    rerender(
      <TaskListProvider>
        <Probe />
        <StatusReasonPanel />
      </TaskListProvider>,
    );
    expect(screen.getByTestId('reasons').textContent).toBe('expand');

    rerender(
      <TaskListProvider>
        <Probe />
      </TaskListProvider>,
    );
    expect(screen.getByTestId('reasons').textContent).toBe('');
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

    it('renders URL projects as external links and stops click propagation', () => {
      const onRowClick = vi.fn();
      render(
        <div onClick={onRowClick}>
          <ProjectCell project="https://github.com/org/repo/" />
        </div>,
      );
      const link = screen.getByRole('link');
      expect(link).toHaveAttribute('href', 'https://github.com/org/repo/');
      expect(link).toHaveAttribute('target', '_blank');
      fireEvent.click(link);
      expect(onRowClick).not.toHaveBeenCalled();
    });
  });

  describe('ViewButton', () => {
    const { ViewButton } = __testing;

    it('navigates to the task detail page and stops click propagation', () => {
      const onRowClick = vi.fn();
      renderInRouter(
        <div onClick={onRowClick}>
          <ViewButton id="task-42" />
        </div>,
      );
      const button = screen.getByRole('link', { name: /view/i });
      expect(button).toHaveAttribute('href', '/task/task-42');
      fireEvent.click(button);
      expect(onRowClick).not.toHaveBeenCalled();
    });
  });
});
