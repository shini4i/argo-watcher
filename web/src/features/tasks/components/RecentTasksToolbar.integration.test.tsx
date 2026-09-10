import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { AdminContext, memoryStore, testDataProvider } from 'react-admin';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { RecentTasksToolbar } from './RecentTasksToolbar';
import { TaskListLayout } from './TaskListLayout';

const identity = vi.fn<() => { data?: { id: string; email?: string }; isLoading: boolean }>();
vi.mock('react-admin', async importOriginal => ({
  ...(await importOriginal<typeof import('react-admin')>()),
  useGetIdentity: () => identity(),
}));

type ListCall = { filter?: Record<string, unknown> };

/**
 * @description Drives the real react-admin list params, whose setFilters
 * REPLACES the filter object. The sibling unit suite supplies a static
 * filterValues, so only this one can catch a second writer dropping the
 * filters a first writer just set.
 */
const renderToolbar = (initialEntry: string) => {
  const getList = vi.fn(() => Promise.resolve({ data: [], total: 0 }));

  const utils = render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <AdminContext dataProvider={testDataProvider({ getList })} store={memoryStore()}>
        <TaskListLayout
          perPageStorageKey="integration.perPage"
          header={<RecentTasksToolbar storageKey="integrationRecent" />}
          listProps={{ storeKey: 'integrationRecent' }}
        >
          <div data-testid="rows" />
        </TaskListLayout>
      </AdminContext>
    </MemoryRouter>,
  );

  const filters = () => getList.mock.calls.map(call => (call[1] as ListCall).filter ?? {});
  return { ...utils, getList, filters, lastFilter: () => filters().at(-1) ?? {} };
};

describe('RecentTasksToolbar on real react-admin list params', () => {
  beforeEach(() => {
    localStorage.clear();
    identity.mockReset();
    identity.mockReturnValue({ data: { id: 'u1', email: 'jane@example.com' }, isLoading: false });
  });

  // The regression this suite exists for: the scope must not replace the filter
  // object that the app/search filters were just written into.
  it('keeps the app filter when the scope adds an author', async () => {
    const { lastFilter } = renderToolbar('/?app=checkout&scope=mine');

    await waitFor(() => {
      expect(lastFilter()).toMatchObject({ app: 'checkout', author: 'jane@example.com' });
    });
  });

  it('keeps the search term alongside the author', async () => {
    const { lastFilter } = renderToolbar('/?search=v1.2.3&scope=mine');

    await waitFor(() => {
      expect(lastFilter()).toMatchObject({ search: 'v1.2.3', author: 'jane@example.com' });
    });
  });

  it('keeps app, status and search together with the author', async () => {
    const { lastFilter } = renderToolbar('/?app=checkout&status=failed&search=nginx&scope=mine');

    await waitFor(() => {
      expect(lastFilter()).toMatchObject({
        app: 'checkout',
        status: 'failed',
        search: 'nginx',
        author: 'jane@example.com',
      });
    });
  });

  it('drops only the author when the reader switches to Everyone', async () => {
    const { lastFilter } = renderToolbar('/?app=checkout&scope=mine');
    await waitFor(() => expect(lastFilter()).toHaveProperty('author'));

    fireEvent.click(screen.getByRole('tab', { name: /Everyone/ }));

    await waitFor(() => {
      expect(lastFilter()).not.toHaveProperty('author');
      expect(lastFilter()).toMatchObject({ app: 'checkout' });
    });
  });

  it('sends no author in anonymous mode', async () => {
    identity.mockReturnValue({ data: undefined, isLoading: false });
    const { filters, lastFilter } = renderToolbar('/?app=checkout');

    await waitFor(() => expect(lastFilter()).toMatchObject({ app: 'checkout' }));
    for (const filter of filters()) {
      expect(filter).not.toHaveProperty('author');
    }
    expect(screen.queryByRole('tablist', { name: 'Deployment scope' })).toBeNull();
  });

  // A cold load resolves the identity after mount, so a Mine chosen earlier has
  // nothing to project onto until then; the author must land once it arrives.
  it('projects the author once a late-arriving identity resolves', async () => {
    identity.mockReturnValue({ data: undefined, isLoading: true });
    const { lastFilter } = renderToolbar('/?app=checkout&scope=mine');
    await waitFor(() => expect(lastFilter()).toMatchObject({ app: 'checkout' }));

    identity.mockReturnValue({ data: { id: 'u1', email: 'jane@example.com' }, isLoading: false });
    fireEvent.click(screen.getByRole('button', { name: /refresh now/i }));

    await waitFor(() => {
      expect(lastFilter()).toMatchObject({ app: 'checkout', author: 'jane@example.com' });
    });
  });

  it('sends no author for a signed-in reader who has not chosen a scope', async () => {
    const { filters, lastFilter } = renderToolbar('/?app=checkout');

    await waitFor(() => expect(lastFilter()).toMatchObject({ app: 'checkout' }));
    // Let any later apply land before asserting the author never appears — the
    // mount filter alone would settle too early to prove it.
    await new Promise(resolve => setTimeout(resolve, 150));

    expect(screen.getByRole('tab', { name: /Everyone/ })).toHaveAttribute('aria-selected', 'true');
    for (const filter of filters()) {
      expect(filter).not.toHaveProperty('author');
    }
  });

  // The cold-load path every signed-in reader hits: the identity landing must
  // not be mistaken for a reason to scope the list.
  it('stays on Everyone when a late-arriving identity resolves', async () => {
    identity.mockReturnValue({ data: undefined, isLoading: true });
    const { filters, lastFilter } = renderToolbar('/?app=checkout');
    await waitFor(() => expect(lastFilter()).toMatchObject({ app: 'checkout' }));

    identity.mockReturnValue({ data: { id: 'u1', email: 'jane@example.com' }, isLoading: false });
    fireEvent.click(screen.getByRole('button', { name: /refresh now/i }));

    await waitFor(() =>
      expect(screen.getByRole('tab', { name: /Everyone/ })).toHaveAttribute(
        'aria-selected',
        'true',
      ),
    );
    expect(lastFilter()).toMatchObject({ app: 'checkout' });
    for (const filter of filters()) {
      expect(filter).not.toHaveProperty('author');
    }
    expect(localStorage.getItem('integrationRecent.scopeChoice')).toBeNull();
  });

  it('drops the author when the identity goes away', async () => {
    const { lastFilter } = renderToolbar('/?app=checkout&scope=mine');
    await waitFor(() => expect(lastFilter()).toHaveProperty('author'));

    identity.mockReturnValue({ data: undefined, isLoading: false });
    fireEvent.click(screen.getByRole('button', { name: /refresh now/i }));

    await waitFor(() => expect(lastFilter()).not.toHaveProperty('author'));
    expect(lastFilter()).toMatchObject({ app: 'checkout' });
    expect(screen.queryByRole('tablist', { name: 'Deployment scope' })).toBeNull();
  });

  // The choice is stored, never the address, so Mine must follow whoever is
  // signed in now rather than pinning the list to a previous reader.
  it('re-points Mine at the new identity when the user changes', async () => {
    const { lastFilter } = renderToolbar('/?scope=mine');
    await waitFor(() => expect(lastFilter()).toMatchObject({ author: 'jane@example.com' }));

    identity.mockReturnValue({ data: { id: 'u2', email: 'sam@example.com' }, isLoading: false });
    fireEvent.click(screen.getByRole('button', { name: /refresh now/i }));

    await waitFor(() => expect(lastFilter()).toMatchObject({ author: 'sam@example.com' }));
    expect(JSON.stringify(localStorage)).not.toContain('@example.com');
  });

  // Recent has no application picker: an app filter only ever arrives from a
  // link (an Overview card, a chip), so remembering it hides the rest of the
  // estate on the next visit for a filter the reader never chose.
  it('does not carry an app filter into a later unfiltered visit', async () => {
    const first = renderToolbar('/?app=checkout');
    await waitFor(() => expect(first.lastFilter()).toMatchObject({ app: 'checkout' }));

    // Any apply mirrors every persisted field, so touch an unrelated filter —
    // this is what used to write the link's app filter to storage for good.
    fireEvent.click(screen.getByRole('tab', { name: /Failed/ }));
    await waitFor(() => expect(first.lastFilter()).toMatchObject({ status: 'failed' }));
    first.unmount();

    const second = renderToolbar('/');
    await waitFor(() => expect(second.getList).toHaveBeenCalled());
    await new Promise(resolve => setTimeout(resolve, 150));

    // The chip is what the reader sees; the filter is what the server is asked.
    expect(screen.queryByText(/Remove filter app/i)).toBeNull();
    expect(second.container.textContent).not.toContain('checkout');
    for (const filter of second.filters()) {
      expect(filter).not.toHaveProperty('app');
    }
  });

  it('settles to a bounded number of list queries', async () => {
    const { getList } = renderToolbar('/?app=checkout');

    await waitFor(() => expect(getList.mock.calls.length).toBeGreaterThan(0));
    await new Promise(resolve => setTimeout(resolve, 250));

    // A filter-write loop would climb without bound here.
    expect(getList.mock.calls.length).toBeLessThan(8);
  });
});
