import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { Location } from 'react-router-dom';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { ListContextProvider } from 'react-admin';
import type { ListContextValue } from 'react-admin';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { Task } from '../../../data/types';
import { RecentTasksToolbar } from './RecentTasksToolbar';

// useRefresh needs a QueryClientProvider ancestor; stub just that hook so the
// toolbar can render against a bare ListContextProvider. Its invalidate-and-
// refetch semantics — what makes the status counts reload with the list — come
// from ra-core/react-query and are out of reach at this level.
const refreshMock = vi.fn();
// useGetIdentity is stubbed for the same reason, and lets a test choose between
// the signed-in and anonymous shape of the toolbar.
const identityMock = vi.fn<() => { data?: { id: string; email?: string } }>();

vi.mock('react-admin', async importOriginal => ({
  ...(await importOriginal<typeof import('react-admin')>()),
  useRefresh: () => refreshMock,
  useGetIdentity: () => identityMock(),
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

vi.mock('./TaskListContext', () => ({
  TaskListProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useTaskListContext: () => ({
    state: {
      pausedReasons: new Set<string>(),
      intervalSec: 30,
      lastRefetchedAt: 0,
    },
    pause: () => {},
    resume: () => {},
    setInterval: () => {},
    markRefetched: () => {},
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

  return { setFilters, ...result };
};

describe('RecentTasksToolbar', () => {
  beforeEach(() => {
    capturedLocation = undefined;
    localStorage.clear();
    refreshMock.mockReset();
    // Anonymous by default, so the pre-existing filter tests see the toolbar
    // without the scope control.
    identityMock.mockReset();
    identityMock.mockReturnValue({ data: undefined });
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

  it('delegates manual refresh to react-admin useRefresh', () => {
    renderToolbar('/');
    fireEvent.click(screen.getByRole('button', { name: /refresh now/i }));
    expect(refreshMock).toHaveBeenCalledTimes(1);
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

  it('hydrates the search term from the URL into filterValues', async () => {
    const { setFilters } = renderToolbar('/tasks?search=checkout');
    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({ search: 'checkout' }, {}, false);
    });
  });

  it('renders the search term as a removable chip and clears it on remove', async () => {
    const { setFilters } = renderToolbar('/tasks?search=checkout');

    const removeButton = screen.getByRole('button', { name: /remove filter search checkout/i });
    expect(screen.getByText(/search:/i)).toBeInTheDocument();
    expect(screen.getByText('checkout')).toBeInTheDocument();

    setFilters.mockReset();
    fireEvent.click(removeButton);
    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({}, {}, false);
    });
    expect(capturedLocation?.search).not.toContain('search=');
  });

  it('typing in the search box commits the term to filterValues', async () => {
    const { setFilters } = renderToolbar('/tasks');
    setFilters.mockReset();

    fireEvent.change(screen.getByTestId('search-input'), { target: { value: 'v1.2.3' } });

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({ search: 'v1.2.3' }, {}, false);
    });
  });

  it('clears the search term when "Clear all" is clicked', async () => {
    const { setFilters } = renderToolbar('/tasks?app=alpha&search=alpha');
    setFilters.mockReset();

    fireEvent.click(screen.getByRole('button', { name: /clear all/i }));
    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({}, {}, false);
    });
  });
});

describe('RecentTasksToolbar scope', () => {
  beforeEach(() => {
    capturedLocation = undefined;
    localStorage.clear();
    refreshMock.mockReset();
    identityMock.mockReset();
    identityMock.mockReturnValue({ data: { id: 'u1', email: 'jane.doe@example.com' } });
  });

  it('defaults a signed-in user to their own deployments', async () => {
    const { setFilters } = renderToolbar('/tasks');

    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith(
        expect.objectContaining({ author: 'jane.doe@example.com' }),
        {},
        false,
      );
    });
  });

  it('hides the scope control and stays global in anonymous mode', async () => {
    identityMock.mockReturnValue({ data: undefined });
    const { setFilters } = renderToolbar('/tasks');

    expect(screen.queryByRole('tablist', { name: 'Deployment scope' })).toBeNull();
    await waitFor(() => expect(setFilters).toHaveBeenCalled());
    for (const call of setFilters.mock.calls) {
      expect(call[0]).not.toHaveProperty('author');
    }
  });

  it('drops the author filter when the user switches to Everyone', async () => {
    const { setFilters } = renderToolbar('/tasks', { author: 'jane.doe@example.com' });

    fireEvent.click(screen.getByRole('tab', { name: /Everyone/ }));

    await waitFor(() => {
      const last = setFilters.mock.calls.at(-1)![0] as Record<string, unknown>;
      expect(last).not.toHaveProperty('author');
    });
  });

  it('mirrors the scope into the URL so a view can be shared', async () => {
    renderToolbar('/tasks');

    fireEvent.click(screen.getByRole('tab', { name: /Everyone/ }));

    await waitFor(() => {
      expect(capturedLocation?.search).toContain('scope=everyone');
    });
  });

  // The address is derived from whoever is signed in now, never persisted, so a
  // restored "mine" cannot scope the list to a previous user.
  it('stores the choice, never the address', async () => {
    renderToolbar('/tasks');

    fireEvent.click(screen.getByRole('tab', { name: /Everyone/ }));

    await waitFor(() => expect(localStorage.getItem('recentTasks.scope')).toBe('everyone'));
    expect(JSON.stringify(localStorage)).not.toContain('jane.doe@example.com');
  });

  it('shows the active scope as a removable chip', async () => {
    const { setFilters } = renderToolbar('/tasks');

    const chip = await screen.findByText(/jane\.doe@example\.com/);
    expect(chip).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /Remove filter author/i }));
    await waitFor(() => {
      const last = setFilters.mock.calls.at(-1)![0] as Record<string, unknown>;
      expect(last).not.toHaveProperty('author');
    });
  });

  it('scopes to the author without touching the search term', async () => {
    const { setFilters } = renderToolbar('/tasks?search=checkout');

    await waitFor(() => {
      const merged = setFilters.mock.calls.map(call => call[0] as Record<string, unknown>);
      expect(merged.some(f => f.search === 'checkout')).toBe(true);
    });
  });
});

describe('RecentTasksToolbar keyboard shortcuts', () => {
  beforeEach(() => {
    capturedLocation = undefined;
    localStorage.clear();
    refreshMock.mockReset();
    identityMock.mockReset();
    identityMock.mockReturnValue({ data: { id: 'u1', email: 'jane.doe@example.com' } });
  });

  it('selects the All tab with "a"', async () => {
    const { setFilters } = renderToolbar('/tasks?status=failed');
    setFilters.mockReset();

    fireEvent.keyDown(document, { key: 'a' });

    await waitFor(() => {
      const last = setFilters.mock.calls.at(-1)![0] as Record<string, unknown>;
      expect(last).not.toHaveProperty('status');
    });
  });

  it('selects the In progress tab with "i"', async () => {
    const { setFilters } = renderToolbar('/tasks');

    fireEvent.keyDown(document, { key: 'i' });

    await waitFor(() => {
      const last = setFilters.mock.calls.at(-1)![0] as Record<string, unknown>;
      expect(last.status).toBe('in progress');
    });
  });

  // "a" and "i" select outright; only "f" toggles, so there is always one key
  // that returns to the unfiltered list.
  it('does not toggle "i" back off on a second press', async () => {
    const { setFilters } = renderToolbar('/tasks?status=in+progress');

    fireEvent.keyDown(document, { key: 'i' });

    await waitFor(() => {
      const last = setFilters.mock.calls.at(-1)![0] as Record<string, unknown>;
      expect(last.status).toBe('in progress');
    });
  });

  it('toggles the Failed tab with "f"', async () => {
    const { setFilters } = renderToolbar('/tasks');

    fireEvent.keyDown(document, { key: 'f' });
    await waitFor(() => {
      const last = setFilters.mock.calls.at(-1)![0] as Record<string, unknown>;
      expect(last.status).toBe('failed');
    });

    fireEvent.keyDown(document, { key: 'f' });
    await waitFor(() => {
      const last = setFilters.mock.calls.at(-1)![0] as Record<string, unknown>;
      expect(last).not.toHaveProperty('status');
    });
  });

  it('toggles the scope with "m"', async () => {
    renderToolbar('/tasks');

    fireEvent.keyDown(document, { key: 'm' });
    await waitFor(() => expect(capturedLocation?.search).toContain('scope=everyone'));

    fireEvent.keyDown(document, { key: 'm' });
    await waitFor(() => expect(capturedLocation?.search).toContain('scope=mine'));
  });

  it('leaves a key alone while an input is focused', async () => {
    const { setFilters } = renderToolbar('/tasks');
    const input = screen.getByTestId('search-input');
    input.focus();

    const callsBefore = setFilters.mock.calls.length;
    fireEvent.keyDown(input, { key: 'f' });

    expect(setFilters.mock.calls).toHaveLength(callsBefore);
  });

  it('leaves a key alone when a modifier is held', async () => {
    const { setFilters } = renderToolbar('/tasks');
    await waitFor(() => expect(setFilters).toHaveBeenCalled());

    const callsBefore = setFilters.mock.calls.length;
    fireEvent.keyDown(document, { key: 'f', ctrlKey: true });

    expect(setFilters.mock.calls).toHaveLength(callsBefore);
  });
});
