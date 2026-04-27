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
    if (typeof value !== 'string') return '';
    const trimmed = value.trim();
    if (!trimmed || trimmed.toLowerCase() === 'null') return '';
    return value;
  },
}));

vi.mock('./RefreshControl', () => ({
  RefreshControl: ({ onRefresh }: { onRefresh: () => void }) => (
    <button type="button" aria-label="refresh now" onClick={onRefresh}>
      refresh
    </button>
  ),
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

const taskListContextState = {
  searchQuery: '',
  setSearchQuery: vi.fn(),
};

vi.mock('./TaskListContext', () => ({
  TaskListProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useTaskListContext: () => ({
    state: {
      pausedReasons: new Set<string>(),
      intervalSec: 30,
      lastRefetchedAt: 0,
      searchQuery: taskListContextState.searchQuery,
    },
    pause: () => {},
    resume: () => {},
    setInterval: () => {},
    markRefetched: () => {},
    setSearchQuery: taskListContextState.setSearchQuery,
    registerClearAll: () => () => {},
    clearAll: () => {},
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
    localStorage.clear();
    taskListContextState.searchQuery = '';
    taskListContextState.setSearchQuery.mockReset();
  });

  it('hydrates the application filter from URL on mount', async () => {
    const { setFilters } = renderToolbar('/tasks?app=alpha');
    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({ app: 'alpha' }, {}, false);
    });
    expect((screen.getByTestId('app-filter') as HTMLInputElement).value).toBe('alpha');
  });

  it('commits app filter changes and merges with existing search params', async () => {
    const { setFilters } = renderToolbar('/tasks?page=2&sort=created');
    setFilters.mockReset();
    const input = screen.getByTestId('app-filter') as HTMLInputElement;

    fireEvent.change(input, { target: { value: 'alpha' } });

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({ app: 'alpha' }, {}, false);
    });
    const params = new URLSearchParams(capturedLocation?.search ?? '');
    expect(params.get('page')).toBe('2');
    expect(params.get('sort')).toBe('created');
    expect(params.get('app')).toBe('alpha');
  });

  it('removes the app param while preserving other params when filter cleared', async () => {
    const { setFilters } = renderToolbar('/tasks?page=3&perPage=50&app=beta');
    const input = screen.getByTestId('app-filter') as HTMLInputElement;

    await waitFor(() => expect(input.value).toBe('beta'));

    setFilters.mockReset();
    fireEvent.change(input, { target: { value: '' } });

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({}, {}, false);
    });
    const params = new URLSearchParams(capturedLocation?.search ?? '');
    expect(params.get('page')).toBe('3');
    expect(params.get('perPage')).toBe('50');
    expect(params.has('app')).toBe(false);
  });

  it('forwards manual refresh to refetch', () => {
    const { refetch } = renderToolbar('/');
    refetch.mockClear();
    fireEvent.click(screen.getByRole('button', { name: /refresh now/i }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it('writes filterValues.status when a status tab is selected', async () => {
    const { setFilters } = renderToolbar('/tasks');
    setFilters.mockReset();
    fireEvent.click(screen.getByRole('button', { name: 'failed' }));
    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({ status: 'failed' }, {}, false);
    });
  });

  it('removes filterValues.status when "all" is selected', async () => {
    const { setFilters } = renderToolbar('/tasks?status=failed');
    setFilters.mockReset();
    fireEvent.click(screen.getByRole('button', { name: 'all' }));
    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({}, {}, false);
    });
  });

  it('renders the search query as a removable chip and clears it on remove', () => {
    taskListContextState.searchQuery = 'checkout';
    renderToolbar('/tasks');

    const removeButton = screen.getByRole('button', { name: /remove filter search checkout/i });
    expect(screen.getByText(/search:/i)).toBeInTheDocument();
    expect(screen.getByText('checkout')).toBeInTheDocument();

    fireEvent.click(removeButton);
    expect(taskListContextState.setSearchQuery).toHaveBeenCalledWith('');
  });

  it('clears the search query when "Clear all" is clicked', () => {
    taskListContextState.searchQuery = 'alpha';
    const { setFilters } = renderToolbar('/tasks?app=alpha');
    setFilters.mockReset();

    fireEvent.click(screen.getByRole('button', { name: /clear all/i }));
    expect(taskListContextState.setSearchQuery).toHaveBeenCalledWith('');
  });
});
