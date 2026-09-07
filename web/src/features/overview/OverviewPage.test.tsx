import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { localStorageStore, StoreContextProvider } from 'react-admin';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { OverviewPage } from './OverviewPage';
import type { AppSummary } from './types';

const httpClient = vi.fn();
vi.mock('../../data/httpClient', async importOriginal => ({
  ...(await importOriginal<typeof import('../../data/httpClient')>()),
  httpClient: (...args: unknown[]) => httpClient(...args),
}));

const notify = vi.fn();
vi.mock('../../shared/hooks/useCopyToClipboard', () => ({
  useCopyToClipboard: () => notify,
}));

const summary = (overrides: Partial<AppSummary> = {}): AppSummary => ({
  app: 'checkout',
  project: 'acme/checkout',
  total: 4,
  failed: 0,
  running: 0,
  deployed: 4,
  median_duration_seconds: 42,
  last_created: 1_780_000_000,
  last_status: 'deployed',
  recent_statuses: ['deployed', 'deployed'],
  ...overrides,
});

const respondWith = (apps: AppSummary[], extra: Record<string, unknown> = {}) => {
  httpClient.mockImplementation(() =>
    Promise.resolve({
      data: { apps, total_apps: apps.length, ...extra },
      status: 200,
      headers: {} as Headers,
    }),
  );
};

const renderPage = () =>
  render(
    <StoreContextProvider value={localStorageStore()}>
      <MemoryRouter initialEntries={['/overview']}>
        <OverviewPage />
      </MemoryRouter>
    </StoreContextProvider>,
  );

describe('OverviewPage', () => {
  beforeEach(() => {
    localStorage.clear();
    httpClient.mockReset();
    notify.mockReset();
    respondWith([]);
  });

  afterEach(() => {
    document.title = '';
  });

  it('sums the window into the KPI strip', async () => {
    respondWith([
      summary({ app: 'a', running: 2, failed: 1, deployed: 5 }),
      summary({ app: 'b', running: 1, failed: 3, deployed: 2 }),
    ]);

    renderPage();

    await waitFor(() => expect(screen.getByText('RUNNING NOW')).toBeInTheDocument());
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('4')).toBeInTheDocument();
    expect(screen.getByText('7')).toBeInTheDocument();
  });

  // The counters come from the backend's own GROUP BY, so no sampling note is
  // warranted — and claiming one would be wrong.
  it('states no sampling caveat', async () => {
    respondWith([summary()]);
    renderPage();

    await waitFor(() => expect(screen.getByText('ALL APPLICATIONS')).toBeInTheDocument());
    expect(screen.queryByText(/last 1 ?000/i)).toBeNull();
  });

  it('cards only the applications needing attention', async () => {
    respondWith([
      summary({ app: 'broken', failed: 2, last_status: 'failed', last_status_reason: 'Error: boom' }),
      summary({ app: 'clean' }),
    ]);

    renderPage();

    await waitFor(() => expect(screen.getByText('NEEDS ATTENTION')).toBeInTheDocument());
    expect(screen.getByText('Failing')).toBeInTheDocument();
    // The clean app is in the list below, not carded.
    expect(screen.getAllByText('clean')).toHaveLength(1);
  });

  it('names the failure on a failing card', async () => {
    respondWith([
      summary({
        app: 'broken',
        failed: 1,
        last_status: 'failed',
        last_status_reason: 'Application deployment failed. Rollout status is not available\n\nSync operation phase: Failed',
      }),
    ]);

    renderPage();

    await waitFor(() => expect(screen.getByText('Application deployment failed. Rollout status is not available')).toBeInTheDocument());
  });

  // `/tasks`, not `/`: react-admin redirects `/` to the first resource's list
  // and drops the query on the way, so `/?app=x` silently arrives unfiltered.
  it('links a card into the task list filtered to that app', async () => {
    respondWith([summary({ app: 'broken', failed: 1, last_status: 'failed' })]);
    renderPage();

    await waitFor(() => expect(screen.getAllByRole('link').length).toBeGreaterThan(0));
    const links = screen.getAllByRole('link').map(link => link.getAttribute('href'));
    expect(links).toContain('/tasks?app=broken');
    expect(links).not.toContain('/?app=broken');
  });

  it('refetches when the window changes', async () => {
    respondWith([summary()]);
    renderPage();

    await waitFor(() => expect(httpClient).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('tab', { name: '30 d' }));

    await waitFor(() => expect(httpClient).toHaveBeenCalledTimes(2));
  });

  // Clearing the rows for the round trip unmounted every section, which read as
  // the page blinking on each window switch.
  it('holds the current numbers on screen while the next window loads', async () => {
    respondWith([summary({ app: 'checkout', running: 2 })]);
    renderPage();

    await waitFor(() => expect(screen.getByText('ALL APPLICATIONS')).toBeInTheDocument());

    let release: (value: unknown) => void = () => {};
    httpClient.mockImplementation(() => new Promise(resolve => { release = resolve; }));
    fireEvent.click(screen.getByRole('tab', { name: '30 d' }));

    // Marked busy, but nothing unmounts and no skeleton replaces the numbers.
    await waitFor(() => expect(document.querySelector('[aria-busy="true"]')).toBeTruthy());
    expect(screen.getByText('ALL APPLICATIONS')).toBeInTheDocument();
    expect(screen.getByText('RUNNING NOW')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();

    await act(async () => {
      release({
        data: { apps: [summary({ app: 'payments', running: 9 })], total_apps: 1 },
        status: 200,
        headers: {} as Headers,
      });
    });

    await waitFor(() => expect(screen.getByText('9')).toBeInTheDocument());
    expect(document.querySelector('[aria-busy="true"]')).toBeNull();
  });

  it('pins an application and keeps it in its own block', async () => {
    respondWith([summary({ app: 'checkout' })]);
    renderPage();

    await waitFor(() => expect(screen.getByText('MY APPS')).toBeInTheDocument());
    expect(screen.getByText(/0 pinned/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /Pin an app/ }));
    // The picker opens with every unpinned app listed, so the option is
    // clickable without typing.
    fireEvent.click(await screen.findByRole('option', { name: 'checkout' }));

    await waitFor(() => expect(screen.getByText(/1 pinned/)).toBeInTheDocument());
    expect(screen.getByRole('button', { name: 'Unpin checkout' })).toBeInTheDocument();
  });

  it('offers no share link until something is pinned', async () => {
    respondWith([summary()]);
    renderPage();

    await waitFor(() => expect(screen.getByText('MY APPS')).toBeInTheDocument());
    expect(screen.getByRole('button', { name: /Copy view link/ })).toBeDisabled();
  });

  it('filters the all-applications list', async () => {
    respondWith([summary({ app: 'checkout' }), summary({ app: 'payments' })]);
    renderPage();

    await waitFor(() => expect(screen.getByText('ALL APPLICATIONS')).toBeInTheDocument());
    fireEvent.change(screen.getByLabelText('Filter applications'), { target: { value: 'pay' } });

    expect(screen.getByText(/1 of 2 in this window/)).toBeInTheDocument();
  });

  it('shows an empty state when the window holds no deployments', async () => {
    respondWith([]);
    renderPage();

    await waitFor(() =>
      expect(screen.getByText('No deployments in this window')).toBeInTheDocument(),
    );
  });

  // Rendering zeros after a failed aggregate would read as "all clear".
  it('reports a failed aggregate instead of showing zeros', async () => {
    respondWith([], { error: 'failed to aggregate app summaries' });
    renderPage();

    await waitFor(() => expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument());
    expect(screen.queryByText('RUNNING NOW')).toBeNull();
  });

  it('retries the fetch from the error state', async () => {
    respondWith([], { error: 'failed to aggregate app summaries' });
    renderPage();

    await waitFor(() => expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument());
    respondWith([summary()]);
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    await waitFor(() => expect(screen.getByText('ALL APPLICATIONS')).toBeInTheDocument());
  });

  it('titles the document', async () => {
    respondWith([summary()]);
    renderPage();
    await waitFor(() => expect(document.title).toBe('Overview — Argo Watcher'));
  });
});
