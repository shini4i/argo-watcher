import { fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DateRangePicker } from './DateRangePicker';

let mockTimezone: 'utc' | 'local' = 'utc';

vi.mock('../../../../shared/providers/TimezoneProvider', () => ({
  useTimezone: () => ({
    timezone: mockTimezone,
    formatDate: (ts: number, opts?: Intl.DateTimeFormatOptions) => {
      const date = new Date(ts * 1000);
      return new Intl.DateTimeFormat('en-GB', { ...opts, timeZone: 'UTC' }).format(date);
    },
  }),
}));

const FIXED_NOW = new Date('2026-04-27T12:00:00Z'); // a Monday

const dayCell = (date: string) =>
  within(screen.getByRole('grid', { name: 'Calendar' })).getByRole('gridcell', { name: date });

describe('DateRangePicker', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(FIXED_NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
    mockTimezone = 'utc';
    process.env.TZ = 'UTC';
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

  it('opens on the month the committed range starts in, not on today', () => {
    const start = Math.floor(Date.parse('2026-02-10T00:00:00Z') / 1000);
    const end = Math.floor(Date.parse('2026-02-14T23:59:59Z') / 1000);
    render(<DateRangePicker value={{ start, end }} onApply={() => {}} />);

    fireEvent.click(screen.getByRole('button', { name: /10.*Feb.*2026/ }));
    expect(screen.getByText('February 2026')).toBeInTheDocument();
  });

  it('discards an uncommitted selection when the popover is reopened', () => {
    render(<DateRangePicker value={{ start: null, end: null }} onApply={() => {}} />);

    fireEvent.click(screen.getByRole('button', { name: /Select date range/ }));
    fireEvent.click(screen.getByRole('option', { name: 'Today' }));
    expect(screen.getByRole('button', { name: 'Apply' })).not.toBeDisabled();

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    fireEvent.click(screen.getByRole('button', { name: /Select date range/ }));
    // A clean draft is what re-disables Apply.
    expect(screen.getByRole('button', { name: 'Apply' })).toBeDisabled();
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
    fireEvent.click(dayCell('27 April 2026'));
    fireEvent.click(dayCell('20 April 2026'));

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    expect(onApply).toHaveBeenCalledWith({
      start: Math.floor(Date.parse('2026-04-20T00:00:00Z') / 1000),
      end: Math.floor(Date.parse('2026-04-27T23:59:59Z') / 1000),
    });
  });

  it('keeps an in-progress selection when the parent re-renders with an equal range', () => {
    // HistoryFilters hands the picker a fresh object literal on every render.
    const onApply = vi.fn();
    const empty = () => <DateRangePicker value={{ start: null, end: null }} onApply={onApply} />;
    const { rerender } = render(empty());

    fireEvent.click(screen.getByRole('button', { name: /Select date range/ }));
    fireEvent.click(dayCell('20 April 2026'));

    rerender(empty());

    expect(dayCell('20 April 2026')).toHaveAttribute('aria-selected', 'true');
    // Closing the range on the next click is what proves pickingStart survived too.
    fireEvent.click(dayCell('27 April 2026'));
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    expect(onApply).toHaveBeenCalledWith({
      start: Math.floor(Date.parse('2026-04-20T00:00:00Z') / 1000),
      end: Math.floor(Date.parse('2026-04-27T23:59:59Z') / 1000),
    });
  });

  it('keeps the browsed month when the parent re-renders with an equal range', () => {
    const empty = () => <DateRangePicker value={{ start: null, end: null }} onApply={() => {}} />;
    const { rerender } = render(empty());

    fireEvent.click(screen.getByRole('button', { name: /Select date range/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Previous month' }));
    expect(screen.getByText('March 2026')).toBeInTheDocument();

    rerender(empty());

    expect(screen.getByText('March 2026')).toBeInTheDocument();
  });

  it('reseeds the draft when the committed range changes while the popover is open', () => {
    const start = Math.floor(Date.parse('2026-02-10T00:00:00Z') / 1000);
    const end = Math.floor(Date.parse('2026-02-14T23:59:59Z') / 1000);
    const { rerender } = render(
      <DateRangePicker value={{ start: null, end: null }} onApply={() => {}} />,
    );

    fireEvent.click(screen.getByRole('button', { name: /Select date range/ }));
    rerender(<DateRangePicker value={{ start, end }} onApply={() => {}} />);

    expect(screen.getByText('February 2026')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Apply' })).toBeDisabled();
  });

  it('reseeds the draft when only the committed end moves while the popover is open', () => {
    const start = Math.floor(Date.parse('2026-02-10T00:00:00Z') / 1000);
    const end = Math.floor(Date.parse('2026-02-14T23:59:59Z') / 1000);
    const laterEnd = Math.floor(Date.parse('2026-02-18T23:59:59Z') / 1000);
    const { rerender } = render(
      <DateRangePicker value={{ start, end }} onApply={() => {}} />,
    );

    fireEvent.click(screen.getByRole('button', { name: /10.*Feb.*2026/ }));
    rerender(<DateRangePicker value={{ start, end: laterEnd }} onApply={() => {}} />);

    // A stale draft would still read as dirty and re-enable Apply.
    expect(screen.getByRole('button', { name: 'Apply' })).toBeDisabled();
  });

  it('names the month and its cells in local time when the timezone is local', () => {
    // 12:00 UTC on 30 April is already 1 May at UTC+14, so a calendar that
    // slipped back to UTC would say April here.
    process.env.TZ = 'Pacific/Kiritimati';
    vi.setSystemTime(new Date('2026-04-30T12:00:00Z'));
    mockTimezone = 'local';
    render(<DateRangePicker value={{ start: null, end: null }} onApply={() => {}} />);

    fireEvent.click(screen.getByRole('button', { name: /Select date range/ }));

    expect(screen.getByText('May 2026')).toBeInTheDocument();
    expect(dayCell('1 May 2026')).toBeInTheDocument();
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
