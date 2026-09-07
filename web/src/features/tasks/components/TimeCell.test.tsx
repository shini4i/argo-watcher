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
    render(<TimeCell ts={null} />);
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('stacks the relative time over the exact clock time', () => {
    const ts = Math.floor(new Date('2026-04-27T14:12:08Z').getTime() / 1000);
    render(<TimeCell ts={ts} />);

    expect(screen.getByText(`relative-${ts}`)).toBeInTheDocument();
    expect(screen.getByText('formatted')).toBeInTheDocument();
  });

  it('shows only hours, minutes and seconds on the exact line', () => {
    const ts = Math.floor(new Date('2026-04-27T14:12:08Z').getTime() / 1000);
    render(<TimeCell ts={ts} />);

    const optionSets = formatDateMock.mock.calls.map(call => call[1] as Intl.DateTimeFormatOptions);
    const clockOnly = optionSets.find(options => options.year === undefined);
    expect(clockOnly).toMatchObject({ hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
  });

  it('carries the full timestamp as a tooltip for the ambiguous relative line', () => {
    const ts = Math.floor(new Date('2026-04-27T14:12:08Z').getTime() / 1000);
    formatDateMock.mockImplementation((_value: number, options: Intl.DateTimeFormatOptions) =>
      options.year ? 'full-timestamp' : 'clock-only',
    );
    render(<TimeCell ts={ts} />);

    expect(screen.getByTitle('full-timestamp')).toBeInTheDocument();
  });
});
