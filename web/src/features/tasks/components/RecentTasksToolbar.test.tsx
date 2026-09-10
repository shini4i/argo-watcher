import { useEffect } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { Location } from 'react-router-dom';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { ListContextProvider } from 'react-admin';
import type { ListContextValue } from 'react-admin';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { Task } from '../../../data/types';
import { blockStorageAccess } from '../../../test/blockStorage';
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

// Only the normalizer is still reached from here: the toolbar renders no
// application picker, and the mock keeps this suite off the Autocomplete.
vi.mock('./ApplicationFilter', () => ({
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
  const location = useLocation();
  useEffect(() => {
    capturedLocation = location;
  }, [location]);
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

  // The URL is the only way in now that the picker is gone: the overview links
  // to `/?app=<name>`, so hydration is the whole contract.
  it('hydrates the application filter from URL on mount', async () => {
    const { setFilters } = renderToolbar('/tasks?app=alpha');
    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith({ app: 'alpha' }, {}, false);
    });
    expect(screen.getByText('alpha')).toBeInTheDocument();
  });

  it('offers no application picker, leaving search as the only typed filter', () => {
    renderToolbar('/tasks');

    expect(screen.queryByTestId('app-filter')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Filter by application')).not.toBeInTheDocument();
  });

  it('removes the app param while preserving other params when the chip is cleared', async () => {
    const { setFilters } = renderToolbar('/tasks?page=3&perPage=50&app=beta');

    await waitFor(() => expect(screen.getByText('beta')).toBeInTheDocument());

    setFilters.mockReset();
    fireEvent.click(screen.getByRole('button', { name: /remove filter app beta/i }));

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

  it('defaults a signed-in user to every deployment', async () => {
    const { setFilters } = renderToolbar('/tasks');

    expect(screen.getByRole('tab', { name: /Everyone/ })).toHaveAttribute('aria-selected', 'true');
    await waitFor(() => expect(setFilters).toHaveBeenCalled());
    for (const call of setFilters.mock.calls) {
      expect(call[0]).not.toHaveProperty('author');
    }
  });

  it('restores a stored "mine" choice on the next visit', async () => {
    localStorage.setItem('recentTasks.scopeChoice', 'mine');
    const { setFilters } = renderToolbar('/tasks');

    expect(screen.getByRole('tab', { name: /Mine/ })).toHaveAttribute('aria-selected', 'true');
    await waitFor(() => {
      expect(setFilters).toHaveBeenCalledWith(
        expect.objectContaining({ author: 'jane.doe@example.com' }),
        {},
        false,
      );
    });
  });

  // readScopeChoice runs while rendering, so an unguarded read would take the
  // list down instead of merely forgetting the scope.
  it('defaults to Everyone and still switches when storage is blocked', async () => {
    const restore = blockStorageAccess();
    try {
      const { setFilters } = renderToolbar('/tasks');

      expect(screen.getByRole('tab', { name: /Everyone/ })).toHaveAttribute(
        'aria-selected',
        'true',
      );

      fireEvent.click(screen.getByRole('tab', { name: /Mine/ }));

      await waitFor(() =>
        expect(setFilters).toHaveBeenLastCalledWith(
          expect.objectContaining({ author: 'jane.doe@example.com' }),
          {},
          false,
        ),
      );
    } finally {
      restore();
    }
  });

  // A link asking for the whole estate wins the visit, but it is not a choice,
  // so it must leave the reader's own Mine standing for next time.
  it('lets a link ask for Everyone without erasing a stored "mine"', async () => {
    localStorage.setItem('recentTasks.scopeChoice', 'mine');
    const { setFilters } = renderToolbar('/tasks?scope=everyone');

    expect(screen.getByRole('tab', { name: /Everyone/ })).toHaveAttribute('aria-selected', 'true');
    await waitFor(() => expect(setFilters).toHaveBeenCalled());
    for (const call of setFilters.mock.calls) {
      expect(call[0]).not.toHaveProperty('author');
    }
    expect(localStorage.getItem('recentTasks.scopeChoice')).toBe('mine');
  });

  // Releases up to 1.3.0 wrote `recentTasks.scope` for readers who never chose
  // it, so that key is not evidence of a choice and must not win the default.
  it('ignores a scope left behind by an earlier release', async () => {
    localStorage.setItem('recentTasks.scope', 'mine');
    const { setFilters } = renderToolbar('/tasks');

    expect(screen.getByRole('tab', { name: /Everyone/ })).toHaveAttribute('aria-selected', 'true');
    await waitFor(() => expect(setFilters).toHaveBeenCalled());
    for (const call of setFilters.mock.calls) {
      expect(call[0]).not.toHaveProperty('author');
    }
  });

  // Persisting the default would make it a "choice", which is what stopped the
  // previous default flip from reaching anyone.
  it('stores nothing when the scope is Everyone', async () => {
    renderToolbar('/tasks');

    fireEvent.click(screen.getByRole('tab', { name: /Mine/ }));
    await waitFor(() => expect(localStorage.getItem('recentTasks.scopeChoice')).toBe('mine'));

    fireEvent.click(screen.getByRole('tab', { name: /Everyone/ }));
    await waitFor(() => expect(localStorage.getItem('recentTasks.scopeChoice')).toBeNull());
  });

  // A scope from a link is not a choice, so an apply the reader made for an
  // unrelated reason must not turn it into their remembered one.
  it('leaves a link-derived scope unwritten when another filter is applied', async () => {
    const { setFilters } = renderToolbar('/tasks?scope=mine');
    await waitFor(() => expect(setFilters).toHaveBeenCalled());

    fireEvent.click(screen.getByRole('button', { name: 'failed' }));

    await waitFor(() => expect(capturedLocation?.search).toContain('status=failed'));
    expect(localStorage.getItem('recentTasks.scopeChoice')).toBeNull();
    expect(localStorage.getItem('recentTasks.app')).toBeNull();
    // The link still scopes the visit it was opened in.
    expect(setFilters.mock.calls.at(-1)![0]).toHaveProperty('author');
  });

  it('erases the remembered choice when everything is cleared', async () => {
    localStorage.setItem('recentTasks.scopeChoice', 'mine');
    const { setFilters } = renderToolbar('/tasks?app=alpha');
    await screen.findByText(/jane\.doe@example\.com/);

    fireEvent.click(screen.getByRole('button', { name: /clear all/i }));

    await waitFor(() => expect(localStorage.getItem('recentTasks.scopeChoice')).toBeNull());
    expect(setFilters.mock.calls.at(-1)![0]).toEqual({});
    expect(screen.getByRole('tab', { name: /Everyone/ })).toHaveAttribute('aria-selected', 'true');
    expect(capturedLocation?.search).not.toContain('scope=');
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

  // An OIDC deployment switched to anonymous, or the gap between sign-out and
  // sign-in, leaves a remembered Mine with no address behind it.
  it('ignores a remembered "mine" when there is no identity', async () => {
    identityMock.mockReturnValue({ data: undefined });
    localStorage.setItem('recentTasks.scopeChoice', 'mine');
    const { setFilters } = renderToolbar('/tasks');

    expect(screen.queryByRole('tablist', { name: 'Deployment scope' })).toBeNull();
    expect(screen.queryByText(/Remove filter author/i)).toBeNull();
    await waitFor(() => expect(setFilters).toHaveBeenCalled());
    for (const call of setFilters.mock.calls) {
      expect(call[0]).not.toHaveProperty('author');
    }
  });

  it('drops the author filter when the user switches back to Everyone', async () => {
    const { setFilters } = renderToolbar('/tasks?scope=mine', { author: 'jane.doe@example.com' });

    fireEvent.click(screen.getByRole('tab', { name: /Everyone/ }));

    await waitFor(() => {
      const last = setFilters.mock.calls.at(-1)![0] as Record<string, unknown>;
      expect(last).not.toHaveProperty('author');
    });
  });

  it('mirrors the scope into the URL so a view can be shared', async () => {
    renderToolbar('/tasks');

    fireEvent.click(screen.getByRole('tab', { name: /Mine/ }));

    await waitFor(() => {
      expect(capturedLocation?.search).toContain('scope=mine');
    });
  });

  // The address is derived from whoever is signed in now, never persisted, so a
  // restored "mine" cannot scope the list to a previous user.
  it('stores the choice, never the address', async () => {
    renderToolbar('/tasks');

    fireEvent.click(screen.getByRole('tab', { name: /Mine/ }));

    await waitFor(() => expect(localStorage.getItem('recentTasks.scopeChoice')).toBe('mine'));
    expect(JSON.stringify(localStorage)).not.toContain('jane.doe@example.com');
  });

  it('shows the active scope as a removable chip', async () => {
    localStorage.setItem('recentTasks.scopeChoice', 'mine');
    const { setFilters } = renderToolbar('/tasks?scope=mine');

    const chip = await screen.findByText(/jane\.doe@example\.com/);
    expect(chip).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /Remove filter author/i }));
    await waitFor(() => {
      const last = setFilters.mock.calls.at(-1)![0] as Record<string, unknown>;
      expect(last).not.toHaveProperty('author');
    });
    // Removing the chip is a scope decision, so it must be remembered as one.
    expect(localStorage.getItem('recentTasks.scopeChoice')).toBeNull();
  });

  it('scopes to the author without touching the search term', async () => {
    const { setFilters } = renderToolbar('/tasks?scope=mine&search=checkout');

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
    await waitFor(() => expect(capturedLocation?.search).toContain('scope=mine'));
    // The shortcut picks a scope like the pills do, so it is remembered too.
    expect(localStorage.getItem('recentTasks.scopeChoice')).toBe('mine');

    // Everyone is the default, so it leaves the URL rather than naming itself.
    fireEvent.keyDown(document, { key: 'm' });
    await waitFor(() => expect(capturedLocation?.search).not.toContain('scope='));
    expect(localStorage.getItem('recentTasks.scopeChoice')).toBeNull();
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
