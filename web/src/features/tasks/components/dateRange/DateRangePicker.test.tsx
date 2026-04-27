import { fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DateRangePicker } from './DateRangePicker';

vi.mock('../../../../shared/providers/TimezoneProvider', () => ({
  useTimezone: () => ({
    timezone: 'utc',
    formatDate: (ts: number, opts?: Intl.DateTimeFormatOptions) => {
      const date = new Date(ts * 1000);
      return new Intl.DateTimeFormat('en-GB', { ...opts, timeZone: 'UTC' }).format(date);
    },
  }),
}));

const FIXED_NOW = new Date('2026-04-27T12:00:00Z'); // a Monday

describe('DateRangePicker', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(FIXED_NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders "Select date range" placeholder when value is empty', () => {
    render(<DateRangePicker value={{ start: null, end: null }} onApply={() => {}} />);
    expect(screen.getByRole('button', { name: /Select date range/ })).toBeInTheDocument();
  });

  it('shows the formatted range on the trigger when both endpoints are set', () => {
    const start = Math.floor(Date.parse('2026-04-20T00:00:00Z') / 1000);
    const end = Math.floor(Date.parse('2026-04-27T23:59:59Z') / 1000);
    render(<DateRangePicker value={{ start, end }} onApply={() => {}} />);
    expect(screen.getByRole('button', { name: /20.*Apr.*2026/ })).toBeInTheDocument();
  });

  it('opens the popover with presets and a calendar grid', () => {
    render(<DateRangePicker value={{ start: null, end: null }} onApply={() => {}} />);
    fireEvent.click(screen.getByRole('button', { name: /Select date range/ }));
    expect(screen.getByRole('listbox', { name: 'Date range presets' })).toBeInTheDocument();
    expect(screen.getByRole('grid', { name: 'Calendar' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Today' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Last 7 days' })).toBeInTheDocument();
  });

  it('disables Apply until a complete and dirty range is selected', () => {
    render(<DateRangePicker value={{ start: null, end: null }} onApply={() => {}} />);
    fireEvent.click(screen.getByRole('button', { name: /Select date range/ }));
    expect(screen.getByRole('button', { name: 'Apply' })).toBeDisabled();
  });

  it('selecting a preset enables Apply and emits the computed range', () => {
    const onApply = vi.fn();
    render(<DateRangePicker value={{ start: null, end: null }} onApply={onApply} />);
    fireEvent.click(screen.getByRole('button', { name: /Select date range/ }));
    fireEvent.click(screen.getByRole('option', { name: 'Last 7 days' }));
    const apply = screen.getByRole('button', { name: 'Apply' });
    expect(apply).not.toBeDisabled();
    fireEvent.click(apply);
    expect(onApply).toHaveBeenCalledWith({
      start: Math.floor(Date.parse('2026-04-21T00:00:00Z') / 1000),
      end: Math.floor(Date.parse('2026-04-27T23:59:59Z') / 1000),
    });
  });

  it('clicking two day cells sets start then end; second-earlier click swaps them', () => {
    const onApply = vi.fn();
    render(<DateRangePicker value={{ start: null, end: null }} onApply={onApply} />);
    fireEvent.click(screen.getByRole('button', { name: /Select date range/ }));
    const grid = screen.getByRole('grid', { name: 'Calendar' });

    // Click "27" (today, Apr 27 in-month) then "20" (earlier, in-month) → swap.
    const cells = within(grid).getAllByRole('gridcell');
    const cell27 = cells.find(c => c.textContent === '27' && !c.hasAttribute('disabled'));
    const cell20 = cells.find(c => c.textContent === '20' && !c.hasAttribute('disabled'));
    fireEvent.click(cell27!);
    fireEvent.click(cell20!);

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    expect(onApply).toHaveBeenCalledWith({
      start: Math.floor(Date.parse('2026-04-20T00:00:00Z') / 1000),
      end: Math.floor(Date.parse('2026-04-27T23:59:59Z') / 1000),
    });
  });

  it('Cancel closes the popover without firing onApply', () => {
    const onApply = vi.fn();
    render(<DateRangePicker value={{ start: null, end: null }} onApply={onApply} />);
    fireEvent.click(screen.getByRole('button', { name: /Select date range/ }));
    fireEvent.click(screen.getByRole('option', { name: 'Today' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onApply).not.toHaveBeenCalled();
  });
});
