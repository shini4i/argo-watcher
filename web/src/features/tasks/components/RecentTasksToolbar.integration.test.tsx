import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { AdminContext, testDataProvider } from 'react-admin';
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

  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <AdminContext dataProvider={testDataProvider({ getList })}>
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
  return { getList, filters, lastFilter: () => filters().at(-1) ?? {} };
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
    const { lastFilter } = renderToolbar('/?app=checkout');

    await waitFor(() => {
      expect(lastFilter()).toMatchObject({ app: 'checkout', author: 'jane@example.com' });
    });
  });

  it('keeps the search term alongside the author', async () => {
    const { lastFilter } = renderToolbar('/?search=v1.2.3');

    await waitFor(() => {
      expect(lastFilter()).toMatchObject({ search: 'v1.2.3', author: 'jane@example.com' });
    });
  });

  it('keeps app, status and search together with the author', async () => {
    const { lastFilter } = renderToolbar('/?app=checkout&status=failed&search=nginx');

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
    const { lastFilter } = renderToolbar('/?app=checkout');
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

  // A cold load resolves the identity after mount; the default must still land.
  it('applies Mine once a late-arriving identity resolves', async () => {
    identity.mockReturnValue({ data: undefined, isLoading: true });
    const { lastFilter } = renderToolbar('/?app=checkout');
    await waitFor(() => expect(lastFilter()).toMatchObject({ app: 'checkout' }));

    identity.mockReturnValue({ data: { id: 'u1', email: 'jane@example.com' }, isLoading: false });
    fireEvent.click(screen.getByRole('button', { name: /refresh now/i }));

    await waitFor(() => {
      expect(lastFilter()).toMatchObject({ app: 'checkout', author: 'jane@example.com' });
    });
  });

  it('honours an explicit ?scope=everyone over the signed-in default', async () => {
    const { filters, lastFilter } = renderToolbar('/?app=checkout&scope=everyone');

    await waitFor(() => expect(lastFilter()).toMatchObject({ app: 'checkout' }));
    for (const filter of filters()) {
      expect(filter).not.toHaveProperty('author');
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
