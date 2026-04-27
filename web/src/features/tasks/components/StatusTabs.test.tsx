import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { StatusTabs } from './StatusTabs';

vi.mock('react-admin', () => ({
  useListContext: () => ({ total: 412 }),
  useGetList: (_resource: string, params: { filter?: { status?: string } }) => {
    if (params.filter?.status === 'in progress') return { total: 3 };
    if (params.filter?.status === 'failed') return { total: 7 };
    return { total: 412 };
  },
}));

describe('StatusTabs', () => {
  it('renders three tabs with their counts', () => {
    render(<StatusTabs value={null} onChange={() => {}} />);
    expect(screen.getByRole('tab', { name: /All/ })).toBeInTheDocument();
    expect(screen.getByText('412')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('7')).toBeInTheDocument();
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
});
