import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { Location } from 'react-router-dom';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { ListContextProvider } from 'react-admin';
import type { ListContextValue } from 'react-admin';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { Task } from '../../../data/types';
import { RecentTasksToolbar } from './RecentTasksToolbar';

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
    if (typeof value !== 'string') {
      return '';
    }
    const trimmed = value.trim();
    if (!trimmed || trimmed.toLowerCase() === 'null') {
      return '';
    }
    return value;
  },
}));

const refreshOnRefreshSpy = vi.fn();

vi.mock('./RefreshControl', () => ({
  RefreshControl: ({ onRefresh }: { onRefresh: () => void }) => {
    refreshOnRefreshSpy();
    return (
      <button type="button" aria-label="refresh now" onClick={onRefresh}>
        refresh
      </button>
    );
  },
}));

vi.mock('./SearchInput', () => ({
  SearchInput: ({ value, onChange }: { value: string; onChange: (next: string) => void }) => (
    <input
      aria-label="search"
      data-testid="search-input"
      value={value}
      onChange={event => onChange(event.target.value)}
    />
  ),
}));

interface StatusTabsMockProps {
  value: string | null;
  onChange: (next: string | null) => void;
}

vi.mock('./StatusTabs', () => ({
  StatusTabs: ({ value, onChange }: StatusTabsMockProps) => (
    <div data-testid="status-tabs" data-value={value ?? ''}>
      <button type="button" onClick={() => onChange(null)}>
        all
      </button>
      <button type="button" onClick={() => onChange('failed')}>
        failed
      </button>
    </div>
  ),
}));

vi.mock('./TaskListContext', () => ({
  TaskListProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useTaskListContext: () => ({
    state: { pausedReasons: new Set(), intervalSec: 30, lastRefetchedAt: 0 },
    pause: () => {},
    resume: () => {},
    setInterval: () => {},
    markRefetched: () => {},
  }),
}));

const sampleTasks: Task[] = [
  { id: '1', created: 1, updated: 2, app: 'alpha', author: 'alice', project: 'proj', images: [] },
  { id: '2', created: 3, updated: 4, app: 'beta', author: 'bob', project: 'proj', images: [] },
];

let capturedLocation: Location | undefined;

const LocationObserver = () => {
  capturedLocation = useLocation();
  return null;
};

const renderToolbar = (initialEntry: string, filterValues: Record<string, unknown> = {}) => {
  const setFilters = vi.fn();
  const refetch = vi.fn();

  const contextValue = {
    data: sampleTasks,
    filterValues,
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
    refreshOnRefreshSpy.mockClear();
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

  it('forwards manual refresh from RefreshControl to refetch', () => {
    const { refetch } = renderToolbar('/');
    refetch.mockClear();
    fireEvent.click(screen.getByRole('button', { name: /refresh now/i }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it('writes filterValues.status when a status tab is selected', async () => {
    const { setFilters } = renderToolbar('/tasks');
    fireEvent.click(screen.getByRole('button', { name: 'failed' }));
    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({ status: 'failed' }, {}, false);
    });
  });

  it('removes filterValues.status when "all" is selected', async () => {
    const { setFilters } = renderToolbar('/tasks', { status: 'failed' });
    setFilters.mockReset();
    fireEvent.click(screen.getByRole('button', { name: 'all' }));
    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({}, {}, false);
    });
  });
});
