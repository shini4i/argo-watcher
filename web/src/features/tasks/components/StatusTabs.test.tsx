import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { StatusTabs } from './StatusTabs';

const useGetListMock = vi.fn();
const listContext: { total: number; filterValues: Record<string, unknown> } = {
  total: 412,
  filterValues: { app: 'demo' },
};

vi.mock('react-admin', () => ({
  useListContext: () => listContext,
  useGetList: (...args: unknown[]) => useGetListMock(...args),
}));

const sampleData = [
  { id: '1', status: 'in progress' },
  { id: '2', status: 'in progress' },
  { id: '3', status: 'in progress' },
  { id: '4', status: 'failed' },
  { id: '5', status: 'failed' },
  { id: '6', status: 'failed' },
  { id: '7', status: 'failed' },
  { id: '8', status: 'failed' },
  { id: '9', status: 'failed' },
  { id: '10', status: 'failed' },
  { id: '11', status: 'deployed' },
];

describe('StatusTabs', () => {
  beforeEach(() => {
    listContext.total = 412;
    listContext.filterValues = { app: 'demo' };
    useGetListMock.mockReset();
    useGetListMock.mockReturnValue({ data: sampleData, total: sampleData.length });
  });

  // The count renders inside the tab button, so each tab's accessible name is
  // "<label> <count>" — asserting on the whole name pins a number to its pill.
  const expectTabCounts = (all: string, inProgress: string, failed: string) => {
    expect(screen.getByRole('tab', { name: `All ${all}` })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: `In progress ${inProgress}` })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: `Failed ${failed}` })).toBeInTheDocument();
  };

  it('renders three tabs with their counts', () => {
    render(<StatusTabs value={null} onChange={() => {}} />);
    expectTabCounts('11', '3', '7');
  });

  it('keeps every count independent of the selected status tab', () => {
    // The list context reports only the tasks matching the active status
    // filter; every pill must come from the status-agnostic count query.
    listContext.total = 7;
    listContext.filterValues = { app: 'demo', status: 'failed' };

    render(<StatusTabs value="failed" onChange={() => {}} />);

    expectTabCounts('11', '3', '7');
  });

  it('marks the active tab as selected', () => {
    render(<StatusTabs value="failed" onChange={() => {}} />);
    expect(screen.getByRole('tab', { name: /Failed/ })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: /All/ })).toHaveAttribute('aria-selected', 'false');
  });

  it('emits null when "All" is clicked', () => {
    const onChange = vi.fn();
    render(<StatusTabs value="failed" onChange={onChange} />);
    fireEvent.click(screen.getByRole('tab', { name: /All/ }));
    expect(onChange).toHaveBeenCalledWith(null);
  });

  it('emits the chosen status when a status tab is clicked', () => {
    const onChange = vi.fn();
    render(<StatusTabs value={null} onChange={onChange} />);
    fireEvent.click(screen.getByRole('tab', { name: /In progress/ }));
    expect(onChange).toHaveBeenCalledWith('in progress');
  });

  it('issues a single useGetList query inheriting parent filters minus status', () => {
    listContext.filterValues = { app: 'demo', status: 'failed' };
    render(<StatusTabs value="failed" onChange={() => {}} />);
    expect(useGetListMock).toHaveBeenCalledTimes(1);
    const [resource, params] = useGetListMock.mock.calls[0];
    expect(resource).toBe('tasks');
    expect(params.filter).toEqual({ app: 'demo' });
    expect(params.filter.status).toBeUndefined();
    expect(params.pagination).toEqual({ page: 1, perPage: 1000 });
  });

  it('shows a placeholder instead of zero when no snapshot has loaded yet', () => {
    useGetListMock.mockReturnValue({ data: undefined });
    render(<StatusTabs value={null} onChange={() => {}} />);
    expect(screen.getAllByText('—')).toHaveLength(3);
    expect(screen.queryByText('0')).toBeNull();
  });

  it('appends "+" to status counts when the loaded page is truncated', () => {
    useGetListMock.mockReturnValue({ data: sampleData, total: 5000 });
    render(<StatusTabs value={null} onChange={() => {}} />);

    // Status pills surface the lower-bound suffix; the All pill is the query's
    // own total and stays exact.
    expectTabCounts('5000', '3+', '7+');
    expect(screen.queryByText('5000+')).toBeNull();
  });

  it('does not suffix counts when data.length matches total', () => {
    useGetListMock.mockReturnValue({ data: sampleData, total: sampleData.length });
    render(<StatusTabs value={null} onChange={() => {}} />);
    expectTabCounts('11', '3', '7');
    expect(screen.queryByText('3+')).toBeNull();
  });

  it('keeps the placeholder when a total arrives without rows', () => {
    useGetListMock.mockReturnValue({ data: undefined, total: 5000 });
    render(<StatusTabs value={null} onChange={() => {}} />);
    expect(screen.getAllByText('—')).toHaveLength(3);
    expect(screen.queryByText('0+')).toBeNull();
  });

  it('renders a genuine zero once a snapshot with no matching tasks loads', () => {
    useGetListMock.mockReturnValue({ data: [], total: 0 });
    render(<StatusTabs value={null} onChange={() => {}} />);
    expect(screen.getAllByText('0')).toHaveLength(3);
    expect(screen.queryByText('—')).toBeNull();
  });
});
