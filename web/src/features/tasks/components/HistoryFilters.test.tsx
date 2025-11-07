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
}));

const sampleTasks: Task[] = [
  {
    id: '1',
    created: 1,
    updated: 2,
    app: 'alpha',
    author: 'alice',
    project: 'proj',
    images: [],
  },
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

const startSeconds = (value: string) => Math.floor(new Date(`${value}T00:00:00Z`).getTime() / 1000);
const endSeconds = (value: string) => Math.floor(new Date(`${value}T23:59:59Z`).getTime() / 1000);

describe('HistoryFilters', () => {
  beforeEach(() => {
    capturedLocation = undefined;
    localStorage.clear();
  });

  it('applies date and application filters while keeping existing params', async () => {
    const { setFilters } = renderFilters('/history?page=5&sort=created');
    const startInput = screen.getByLabelText('Start date') as HTMLInputElement;
    const endInput = screen.getByLabelText('End date') as HTMLInputElement;
    const appInput = screen.getByTestId('app-filter') as HTMLInputElement;
    const applyButton = screen.getByRole('button', { name: /apply/i });

    setFilters.mockReset();

    fireEvent.change(startInput, { target: { value: '2024-02-10' } });
    fireEvent.change(endInput, { target: { value: '2024-02-12' } });
    fireEvent.change(appInput, { target: { value: 'alpha' } });
    fireEvent.click(applyButton);

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith(
        { start: startSeconds('2024-02-10'), end: endSeconds('2024-02-12'), app: 'alpha' },
        {},
        false,
      );
    });

    await waitFor(() => {
      const params = new URLSearchParams(capturedLocation?.search ?? '');
      expect(params.get('page')).toBe('5');
      expect(params.get('sort')).toBe('created');
      expect(params.get('startDate')).toBe('2024-02-10');
      expect(params.get('endDate')).toBe('2024-02-12');
      expect(params.get('app')).toBe('alpha');
    });
  });

  it('removes application filter from query string while keeping date and other params', async () => {
    const { setFilters } = renderFilters('/history?page=2&startDate=2024-03-01&endDate=2024-03-05&app=beta');
    const appInput = screen.getByTestId('app-filter') as HTMLInputElement;
    const applyButton = screen.getByRole('button', { name: /apply/i });

    // Ensure date inputs reflect initial query params to satisfy the apply button constraint.
    const startInput = screen.getByLabelText('Start date') as HTMLInputElement;
    const endInput = screen.getByLabelText('End date') as HTMLInputElement;

    await waitFor(() => expect(appInput.value).toBe('beta'));

    fireEvent.change(appInput, { target: { value: '' } });
    await waitFor(() => expect(appInput.value).toBe(''));

    setFilters.mockClear();
    fireEvent.click(applyButton);

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith(
        {
          start: startSeconds(startInput.value),
          end: endSeconds(endInput.value),
        },
        {},
        false,
      );
    });

    await waitFor(() => {
      const params = new URLSearchParams(capturedLocation?.search ?? '');
      expect(params.get('page')).toBe('2');
      expect(params.get('startDate')).toBe('2024-03-01');
      expect(params.get('endDate')).toBe('2024-03-05');
      expect(params.has('app')).toBe(false);
    });
  });

  it('enables the Apply button when only the application filter is provided', async () => {
    const { setFilters } = renderFilters('/history');
    const appInput = screen.getByTestId('app-filter') as HTMLInputElement;
    const applyButton = screen.getByRole('button', { name: /apply/i });

    expect(applyButton).toBeDisabled();

    fireEvent.change(appInput, { target: { value: 'gamma' } });

    await waitFor(() => expect(applyButton).toBeEnabled());

    fireEvent.click(applyButton);

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({ app: 'gamma' }, {}, false);
    });
  });

  it('enables the Apply button when clearing an existing application filter without dates', async () => {
    const { setFilters } = renderFilters('/history?app=delta');
    const appInput = screen.getByTestId('app-filter') as HTMLInputElement;
    const applyButton = screen.getByRole('button', { name: /apply/i });

    await waitFor(() => expect(appInput.value).toBe('delta'));
    expect(applyButton).toBeDisabled();

    fireEvent.change(appInput, { target: { value: '' } });
    await waitFor(() => expect(appInput.value).toBe(''));
    await waitFor(() => expect(applyButton).toBeEnabled());

    fireEvent.click(applyButton);

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({}, {}, false);
    });
  });
});
