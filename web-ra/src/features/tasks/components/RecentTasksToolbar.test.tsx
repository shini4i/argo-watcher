import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { Location } from 'react-router-dom';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { ListContextProvider } from 'react-admin';
import type { ListContextValue } from 'react-admin';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { Task } from '../../../data/types';
import { RecentTasksToolbar } from './RecentTasksToolbar';

const useAutoRefreshSpy = vi.fn();
let latestAutoRefreshHandler: (() => void) | undefined;

vi.mock('../../../shared/hooks/useAutoRefresh', () => ({
  useAutoRefresh: (interval: number, handler: () => void) => {
    useAutoRefreshSpy(interval);
    latestAutoRefreshHandler = handler;
  },
}));

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
  {
    id: '2',
    created: 3,
    updated: 4,
    app: 'beta',
    author: 'bob',
    project: 'proj',
    images: [],
  },
];

let capturedLocation: Location | undefined;

const LocationObserver = () => {
  capturedLocation = useLocation();
  return null;
};

const renderToolbar = (initialEntry: string) => {
  const setFilters = vi.fn();
  const refetch = vi.fn();

  const contextValue = {
    data: sampleTasks,
    filterValues: {},
    setFilters,
    refetch,
  } as unknown as ListContextValue<Task>;

  const result = render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <LocationObserver />
      <ListContextProvider value={contextValue}>
        <RecentTasksToolbar />
      </ListContextProvider>
    </MemoryRouter>,
  );

  return { setFilters, refetch, ...result };
};

describe('RecentTasksToolbar', () => {
  beforeEach(() => {
    capturedLocation = undefined;
    latestAutoRefreshHandler = undefined;
    useAutoRefreshSpy.mockClear();
    localStorage.clear();
  });

  it('merges application filter changes with existing search params', async () => {
    const { setFilters } = renderToolbar('/tasks?page=2&sort=created');
    const input = screen.getByTestId('app-filter') as HTMLInputElement;
    setFilters.mockReset();

    fireEvent.change(input, { target: { value: 'alpha' } });

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({ app: 'alpha' }, {}, false);
    });

    await waitFor(() => {
      const params = new URLSearchParams(capturedLocation?.search ?? '');
      expect(params.get('page')).toBe('2');
      expect(params.get('sort')).toBe('created');
      expect(params.get('app')).toBe('alpha');
    });
  });

  it('removes the app param while preserving other params when filter cleared', async () => {
    const { setFilters } = renderToolbar('/tasks?page=3&perPage=50&app=beta');
    const input = screen.getByTestId('app-filter') as HTMLInputElement;

    await waitFor(() => expect(input.value).toBe('beta'));

    fireEvent.change(input, { target: { value: '' } });
    await waitFor(() => expect(input.value).toBe(''));

    await waitFor(() => {
      const params = new URLSearchParams(capturedLocation?.search ?? '');
      expect(params.get('page')).toBe('3');
      expect(params.get('perPage')).toBe('50');
      expect(params.has('app')).toBe(false);
    });

    expect(setFilters).toHaveBeenCalledTimes(1);
    expect(setFilters.mock.calls[0]).toEqual([{ app: 'beta' }, {}, false]);
  });

  it('invokes manual refresh when clicking the refresh button', async () => {
    const { refetch } = renderToolbar('/');
    const refreshButton = screen.getByRole('button', { name: /refresh now/i });

    refetch.mockClear();
    fireEvent.click(refreshButton);

    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it('wires auto-refresh handler so interval ticks trigger refetch', async () => {
    const { refetch } = renderToolbar('/');

    await waitFor(() => expect(useAutoRefreshSpy).toHaveBeenCalled());
    refetch.mockClear();

    latestAutoRefreshHandler?.();

    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it('updates the refresh interval selection, persists it, and allows disabling auto refresh', async () => {
    renderToolbar('/tasks');

    await waitFor(() => expect(useAutoRefreshSpy).toHaveBeenCalledWith(30));

    const selectControl = screen.getByRole('combobox');
    fireEvent.mouseDown(selectControl);
    const tenOption = await screen.findByRole('option', { name: '10s' });
    fireEvent.click(tenOption);

    await waitFor(() => {
      expect(useAutoRefreshSpy).toHaveBeenCalledWith(10);
    });
    expect(window.localStorage.getItem('recentTasks.refreshInterval')).toBe('10');

    const updatedSelectControl = screen.getByRole('combobox');
    fireEvent.mouseDown(updatedSelectControl);
    const offOption = await screen.findByRole('option', { name: 'Off' });
    fireEvent.click(offOption);

    await waitFor(() => {
      expect(useAutoRefreshSpy).toHaveBeenCalledWith(0);
    });
    expect(window.localStorage.getItem('recentTasks.refreshInterval')).toBe('0');
  });
});
