import { act, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { forwardRef } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Task } from '../../../data/types';
import { TasksDatagrid, __testing } from './TasksDatagrid';

const formatDurationMock = vi.fn((seconds: number) => `${seconds}s`);
const formatRelativeTimeMock = vi.fn((value?: number | null) => `relative-${value}`);
const getBrowserWindowMock = vi.fn(() => globalThis.window as Window);

vi.mock('../../../shared/utils', async () => {
  const actual = await vi.importActual<typeof import('../../../shared/utils')>('../../../shared/utils');
  return {
    ...actual,
    formatDuration: (...args: [number]) => formatDurationMock(...args),
    formatRelativeTime: (...args: [number | undefined]) => formatRelativeTimeMock(...args),
    getBrowserWindow: () => getBrowserWindowMock(),
  };
});

vi.mock('../utils/statusPresentation', () => ({
  describeTaskStatus: vi.fn(() => ({
    label: 'Healthy',
    chipColor: 'success',
    icon: <span data-testid="status-icon" />,
  })),
}));

const formatDateMock = vi.fn((value: number) => `formatted-${value}`);

vi.mock('../../../shared/providers/TimezoneProvider', () => ({
  useTimezone: () => ({
    formatDate: formatDateMock,
  }),
}));

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
  status: 'ok',
  status_reason: 'all green',
};

const datagridPropsLog: Array<Record<string, unknown>> = [];

vi.mock('react-router-dom', () => ({
  Link: forwardRef<HTMLAnchorElement, { children: ReactNode }>((props, ref) => (
    <a ref={ref} {...props}>
      {props.children}
    </a>
  )),
}));

vi.mock('react-admin', () => ({
  Datagrid: (props: Record<string, unknown>) => {
    datagridPropsLog.push(props);
    return <div data-testid="datagrid">{props.children as ReactNode}</div>;
  },
  TextField: ({ label }: { label: string }) => <div data-testid={`text-${label}`}>{label}</div>,
  FunctionField: ({ label, render }: { label: string; render: (record: Task) => ReactNode }) => (
    <div data-testid={`function-${label}`}>{render(sampleRecord)}</div>
  ),
  useRecordContext: () => sampleRecord,
}));

describe('TasksDatagrid', () => {
  beforeEach(() => {
    datagridPropsLog.length = 0;
    formatDateMock.mockClear();
    formatDurationMock.mockClear();
    formatRelativeTimeMock.mockClear();
    getBrowserWindowMock.mockReset();
    getBrowserWindowMock.mockReturnValue(globalThis.window as Window);
  });

  it('configures Datagrid and renders status chips, dates, and links', () => {
    render(<TasksDatagrid />);

    expect(screen.getByTestId('datagrid')).toBeInTheDocument();
    const props = datagridPropsLog.at(-1);
    expect(props).toMatchObject({
      rowClick: 'expand',
      bulkActionButtons: false,
    });
    expect(typeof props?.isRowExpandable).toBe('function');
    expect(formatDateMock).toHaveBeenCalledWith(sampleRecord.created);
    expect(formatRelativeTimeMock).toHaveBeenCalledWith(sampleRecord.updated);
    expect(screen.getByTestId('status-icon')).toBeInTheDocument();
  });

  it('renders project references and images list variants', () => {
    const { ProjectReference, ImagesList } = __testing;

    const { rerender } = render(<ProjectReference project={null} />);
    expect(screen.getByText('—')).toBeInTheDocument();

    rerender(<ProjectReference project="service-api" />);
    expect(screen.getByText('service-api')).toBeInTheDocument();

    rerender(<ProjectReference project="https://github.com/org/repo/" />);
    expect(screen.getByRole('link')).toHaveAttribute('href', 'https://github.com/org/repo/');

    render(<ImagesList images={[]} />);
    expect(screen.getByText('—')).toBeInTheDocument();

    rerender(
      <ImagesList
        images={[
          { image: 'api', tag: '1' },
          { image: 'worker', tag: '2' },
        ]}
      />,
    );
    expect(screen.getByText('api:1')).toBeInTheDocument();
    expect(screen.getByText('worker:2')).toBeInTheDocument();
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

  it('computes live durations for in-progress tasks', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2025-01-01T00:00:00Z'));
    const { DurationField } = __testing;
    const baseRecord: Task = {
      ...sampleRecord,
      status: 'in progress',
      created: 100,
      updated: null as unknown as number,
    };
    const clearIntervalMock = vi.fn();
    const intervalCallbacks: Array<() => void> = [];
    getBrowserWindowMock
      .mockReturnValueOnce({
        setInterval: (cb: () => void) => {
          intervalCallbacks.push(cb);
          return 42 as unknown as number;
        },
        clearInterval: clearIntervalMock,
      } as unknown as Window)
      .mockReturnValue(globalThis.window as Window);

    const { rerender } = render(<DurationField record={baseRecord} />);
    expect(formatDurationMock).toHaveBeenCalledWith(expect.any(Number));

    vi.setSystemTime(new Date('2025-01-01T00:00:05Z'));
    await act(async () => {
      intervalCallbacks[0]?.();
    });
    expect(formatDurationMock).toHaveBeenCalledTimes(2);

    rerender(<DurationField record={{ ...baseRecord, status: 'completed', updated: 150 }} />);
    expect(formatDurationMock).toHaveBeenCalledWith(50);
    expect(clearIntervalMock).toHaveBeenCalled();
    vi.useRealTimers();
  });
});
