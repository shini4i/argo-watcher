import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { AdminContext, HttpError, testDataProvider, useListContext, useRefresh } from 'react-admin';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import { RecentTasksToolbar } from './RecentTasksToolbar';
import { TaskListLayout } from './TaskListLayout';

const providerUnavailable = () =>
  new HttpError('authentication provider unavailable', 503, {
    status: 'authentication provider unavailable',
    error: 'token validation failed',
  });

/** Marks the moment the list query has actually observed a failure. */
const ErrorProbe = () => {
  const { error } = useListContext();
  return error ? <div data-testid="list-error" /> : null;
};

/** Stands in for the toolbar's auto-refresh, which reloads through this same hook. */
const RefreshButton = () => {
  const refresh = useRefresh();
  return (
    <button type="button" onClick={refresh}>
      refresh now
    </button>
  );
};

/**
 * @description Drives the real react-admin list plumbing, whose `total` is left
 * undefined on a failed query and whose rows survive a failed refetch. The sibling
 * suite stubs useListContext, so only this one can hold those two facts honest.
 */
const renderList = (getList: () => Promise<unknown>) =>
  render(
    <MemoryRouter>
      <AdminContext dataProvider={testDataProvider({ getList: vi.fn(getList) })}>
        <TaskListLayout
          perPageStorageKey="test.perPage"
          header={[<ErrorProbe key="probe" />, <RefreshButton key="refresh" />]}
          emptyComponent={<div data-testid="empty-state" />}
        >
          <div data-testid="datagrid">Rows</div>
        </TaskListLayout>
      </AdminContext>
    </MemoryRouter>,
  );

describe('TaskListLayout against react-admin', () => {
  it('names the identity-provider outage when the server refuses every read with 503', async () => {
    renderList(() => Promise.reject(providerUnavailable()));

    await screen.findByText('Argo Watcher cannot verify your session');
    expect(screen.getByText(/could not reach the identity provider/)).toBeInTheDocument();
    expect(screen.getByText(/OIDC issuer is reachable/)).toBeInTheDocument();
    expect(screen.queryByTestId('datagrid')).not.toBeInTheDocument();
    expect(screen.queryByTestId('empty-state')).not.toBeInTheDocument();
  });

  it('reports an aborted request as an unreachable server', async () => {
    renderList(() => Promise.reject(new HttpError('Request timed out', 0)));

    await screen.findByText('Could not reach the Argo Watcher server');
    expect(screen.queryByTestId('datagrid')).not.toBeInTheDocument();
    expect(screen.queryByTestId('empty-state')).not.toBeInTheDocument();
  });

  // The gate keys on absent rows, which is only safe because react-admin retains the
  // loaded page across a failed refetch. Asserted here rather than assumed.
  it('keeps the loaded grid when a refresh fails', async () => {
    let attempt = 0;
    renderList(() => {
      attempt += 1;
      return attempt === 1
        ? Promise.resolve({ data: [{ id: 'task-1' }], total: 1 })
        : Promise.reject(providerUnavailable());
    });

    await screen.findByTestId('datagrid');

    screen.getByRole('button', { name: 'refresh now' }).click();

    // Wait for the failure to be observed, not merely started, so the assertions
    // below cannot pass while the query is still in flight.
    await screen.findByTestId('list-error');

    expect(screen.getByTestId('datagrid')).toBeInTheDocument();
    expect(screen.queryByText('Argo Watcher cannot verify your session')).not.toBeInTheDocument();
  });

  // Search must reach the backend, not narrow the loaded page: a client-side
  // filter would only ever find tasks on the page the user is already looking at.
  it('sends a typed search term to the data provider', async () => {
    const getList = vi.fn(() => Promise.resolve({ data: [{ id: 'task-1' }], total: 1 }));

    render(
      <MemoryRouter>
        <AdminContext dataProvider={testDataProvider({ getList })}>
          <TaskListLayout
            perPageStorageKey="test.perPage"
            header={<RecentTasksToolbar storageKey="searchIntegration" />}
          >
            <div data-testid="datagrid">Rows</div>
          </TaskListLayout>
        </AdminContext>
      </MemoryRouter>,
    );

    await screen.findByTestId('datagrid');
    // jsdom reports a narrow viewport, where the input starts collapsed.
    fireEvent.click(await screen.findByLabelText('Open search'));
    fireEvent.change(await screen.findByLabelText('Search tasks'), {
      target: { value: 'checkout' },
    });

    // The timeout clears SearchInput's own debounce, which gates the commit.
    await waitFor(
      () => {
        expect(getList).toHaveBeenCalledWith(
          'tasks',
          expect.objectContaining({ filter: expect.objectContaining({ search: 'checkout' }) }),
        );
      },
      { timeout: 2000 },
    );
  });
});
