import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { StatusTabs } from './StatusTabs';

const useGetListMock = vi.fn();

vi.mock('react-admin', () => ({
  useListContext: () => ({ total: 412 }),
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
    useGetListMock.mockReset();
    useGetListMock.mockReturnValue({ data: sampleData });
  });

  it('renders three tabs with their counts', () => {
    render(<StatusTabs value={null} onChange={() => {}} />);
    expect(screen.getByRole('tab', { name: /All/ })).toBeInTheDocument();
    expect(screen.getByText('412')).toBeInTheDocument(); // All
    expect(screen.getByText('3')).toBeInTheDocument(); // In progress
    expect(screen.getByText('7')).toBeInTheDocument(); // Failed
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

  it('issues a single useGetList query (no status filter) for all counts', () => {
    render(<StatusTabs value={null} onChange={() => {}} />);
    expect(useGetListMock).toHaveBeenCalledTimes(1);
    const [resource, params] = useGetListMock.mock.calls[0];
    expect(resource).toBe('tasks');
    expect(params.filter).toBeUndefined();
    expect(params.pagination).toEqual({ page: 1, perPage: 1000 });
  });

  it('shows zero counts when no data is loaded yet', () => {
    useGetListMock.mockReturnValue({ data: undefined });
    render(<StatusTabs value={null} onChange={() => {}} />);
    const counts = screen.getAllByText('0');
    expect(counts.length).toBeGreaterThanOrEqual(2);
  });

  it('appends "+" to status counts when the loaded page is truncated', () => {
    useGetListMock.mockReturnValue({ data: sampleData, total: 5000 });
    render(<StatusTabs value={null} onChange={() => {}} />);

    // Status pills surface the lower-bound suffix; the All pill comes from
    // useListContext.total and stays exact.
    expect(screen.getByText('3+')).toBeInTheDocument(); // In progress
    expect(screen.getByText('7+')).toBeInTheDocument(); // Failed
    expect(screen.getByText('412')).toBeInTheDocument(); // All — no "+"
    expect(screen.queryByText('412+')).toBeNull();
  });

  it('does not suffix counts when data.length matches total', () => {
    useGetListMock.mockReturnValue({ data: sampleData, total: sampleData.length });
    render(<StatusTabs value={null} onChange={() => {}} />);
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('7')).toBeInTheDocument();
    expect(screen.queryByText('3+')).toBeNull();
  });

  it('does not suffix loading-state counts when data is undefined', () => {
    useGetListMock.mockReturnValue({ data: undefined, total: 5000 });
    render(<StatusTabs value={null} onChange={() => {}} />);
    const counts = screen.getAllByText('0');
    expect(counts.length).toBeGreaterThan(0);
    expect(screen.queryByText('0+')).toBeNull();
  });
});
