import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { Location } from 'react-router-dom';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { ListContextProvider } from 'react-admin';
import type { ListContextValue } from 'react-admin';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Task } from '../../../data/types';
import { HistoryFilters } from './HistoryFilters';

vi.mock('./ApplicationFilter', () => ({
  ApplicationFilter: ({ value, onChange }: { value: string; onChange: (next: string) => void }) => (
    <input
      aria-label="Application"
      data-testid="app-filter"
      value={value}
      onChange={event => onChange(event.target.value)}
    />
  ),
  readInitialApplication: () => '',
  normalizeApplicationFilterValue: (value?: string | null) => {
    if (typeof value !== 'string') return '';
    const trimmed = value.trim();
    if (!trimmed || trimmed.toLowerCase() === 'null') return '';
    return value;
  },
}));

interface DateRangePickerMockProps {
  value: { start: number | null; end: number | null };
  onApply: (next: { start: number | null; end: number | null }) => void;
}

vi.mock('./dateRange/DateRangePicker', () => ({
  DateRangePicker: ({ value, onApply }: DateRangePickerMockProps) => (
    <div data-testid="date-range">
      <span data-testid="date-start">{value.start ?? ''}</span>
      <span data-testid="date-end">{value.end ?? ''}</span>
      <button
        type="button"
        onClick={() => onApply({ start: 1700000000, end: 1700100000 })}
      >
        choose-range
      </button>
      <button
        type="button"
        onClick={() => onApply({ start: null, end: null })}
      >
        clear-range
      </button>
    </div>
  ),
}));

vi.mock('../../../shared/providers/TimezoneProvider', () => ({
  useTimezone: () => ({
    timezone: 'utc',
    formatDate: (ts: number) => `ts-${ts}`,
  }),
}));

const sampleTasks: Task[] = [
  { id: '1', created: 1, updated: 2, app: 'alpha', author: 'alice', project: 'proj', images: [] },
];

let capturedLocation: Location | undefined;

const LocationObserver = () => {
  capturedLocation = useLocation();
  return null;
};

const renderFilters = (initialEntry: string) => {
  const setFilters = vi.fn();

  const contextValue = {
    data: sampleTasks,
    filterValues: {},
    setFilters,
  } as unknown as ListContextValue<Task>;

  const result = render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <LocationObserver />
      <ListContextProvider value={contextValue}>
        <HistoryFilters />
      </ListContextProvider>
    </MemoryRouter>,
  );

  return { setFilters, ...result };
};

describe('HistoryFilters', () => {
  beforeEach(() => {
    capturedLocation = undefined;
    localStorage.clear();
  });

  it('hydrates filters from URL query parameters on mount', async () => {
    const { setFilters } = renderFilters('/history?startDate=1700000000&endDate=1700100000&app=demo');
    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith(
        { app: 'demo', start: 1700000000, end: 1700100000 },
        {},
        false,
      );
    });
  });

  it('commits the application filter immediately and syncs URL params', async () => {
    const { setFilters } = renderFilters('/history?page=2');
    setFilters.mockReset();
    const appInput = screen.getByTestId('app-filter') as HTMLInputElement;

    fireEvent.change(appInput, { target: { value: 'alpha' } });

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({ app: 'alpha' }, {}, false);
    });
    const params = new URLSearchParams(capturedLocation?.search ?? '');
    expect(params.get('page')).toBe('2');
    expect(params.get('app')).toBe('alpha');
  });

  it('commits a date range when the picker fires Apply', async () => {
    const { setFilters } = renderFilters('/history');
    setFilters.mockReset();

    fireEvent.click(screen.getByRole('button', { name: 'choose-range' }));

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith(
        { start: 1700000000, end: 1700100000 },
        {},
        false,
      );
    });
    const params = new URLSearchParams(capturedLocation?.search ?? '');
    expect(params.get('startDate')).toBe('1700000000');
    expect(params.get('endDate')).toBe('1700100000');
  });

  it('shows active filter chips and clears the range when the chip is removed', async () => {
    const { setFilters } = renderFilters('/history?startDate=1700000000&endDate=1700100000');
    expect(await screen.findByText(/range:/i)).toBeInTheDocument();

    setFilters.mockReset();
    fireEvent.click(screen.getByRole('button', { name: /Remove filter range/ }));

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({}, {}, false);
    });
    const params = new URLSearchParams(capturedLocation?.search ?? '');
    expect(params.has('startDate')).toBe(false);
    expect(params.has('endDate')).toBe(false);
  });

  it('Clear all wipes both the range and app filters', async () => {
    const { setFilters } = renderFilters('/history?startDate=1700000000&endDate=1700100000&app=alpha');
    setFilters.mockReset();

    fireEvent.click(screen.getByRole('button', { name: 'Clear all' }));

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({}, {}, false);
    });
    const params = new URLSearchParams(capturedLocation?.search ?? '');
    expect(params.has('app')).toBe(false);
    expect(params.has('startDate')).toBe(false);
    expect(params.has('endDate')).toBe(false);
  });
});
