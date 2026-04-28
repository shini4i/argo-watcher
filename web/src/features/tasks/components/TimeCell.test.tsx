import { render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { TimeCell } from './TimeCell';

const formatDateMock = vi.fn();

vi.mock('../../../shared/providers/TimezoneProvider', () => ({
  useTimezone: () => ({ formatDate: formatDateMock }),
}));

vi.mock('../../../shared/utils/time', () => ({
  formatRelativeTime: (value: number | null | undefined) => `relative-${value}`,
}));

describe('TimeCell', () => {
  beforeEach(() => {
    formatDateMock.mockReset();
    formatDateMock.mockImplementation(() => 'formatted');
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-04-27T12:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders an em-dash when ts is missing', () => {
    render(<TimeCell ts={null} relative={null} />);
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('renders the formatted date and relative line for current-year timestamps in "both" mode', () => {
    const ts = Math.floor(new Date('2026-04-27T14:12:08Z').getTime() / 1000);
    render(<TimeCell ts={ts} relative={ts} />);

    expect(formatDateMock).toHaveBeenCalledWith(
      ts,
      expect.objectContaining({ day: '2-digit', month: 'short' }),
    );
    const passed = formatDateMock.mock.calls[0][1] as Intl.DateTimeFormatOptions;
    expect(passed.year).toBeUndefined();
    expect(screen.getByText('formatted')).toBeInTheDocument();
    expect(screen.getByText(`relative-${ts}`)).toBeInTheDocument();
  });

  it('includes the year for non-current-year timestamps in "both" mode', () => {
    const ts = Math.floor(new Date('2024-04-27T14:12:08Z').getTime() / 1000);
    render(<TimeCell ts={ts} relative={ts} />);
    const passed = formatDateMock.mock.calls[0][1] as Intl.DateTimeFormatOptions;
    expect(passed.year).toBe('numeric');
  });

  it('renders only the formatted date with year in "date" mode', () => {
    const ts = Math.floor(new Date('2026-04-27T14:12:08Z').getTime() / 1000);
    render(<TimeCell ts={ts} mode="date" />);

    const passed = formatDateMock.mock.calls[0][1] as Intl.DateTimeFormatOptions;
    expect(passed.year).toBe('numeric');
    expect(passed.second).toBe('2-digit');
    expect(screen.getByText('formatted')).toBeInTheDocument();
    expect(screen.queryByText(`relative-${ts}`)).toBeNull();
  });

  it('renders only the relative line in "relative" mode', () => {
    const ts = Math.floor(new Date('2026-04-27T14:12:08Z').getTime() / 1000);
    render(<TimeCell ts={ts} mode="relative" />);

    expect(formatDateMock).not.toHaveBeenCalled();
    expect(screen.getByText(`relative-${ts}`)).toBeInTheDocument();
  });

  it('falls back to ts when relative is omitted in "relative" mode', () => {
    const ts = Math.floor(new Date('2026-04-27T14:12:08Z').getTime() / 1000);
    render(<TimeCell ts={ts} mode="relative" />);
    expect(screen.getByText(`relative-${ts}`)).toBeInTheDocument();
  });
});
