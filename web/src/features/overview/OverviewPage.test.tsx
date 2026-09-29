import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { localStorageStore, StoreContextProvider } from 'react-admin';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { OverviewPage } from './OverviewPage';
import type { AppSummary } from './types';
import { tokens } from '../../theme/tokens';

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

/** Deployed last, but failed three of its last four outcomes. */
const flaky = summary({
  app: 'flaky',
  failed: 3,
  deployed: 1,
  last_status: 'deployed',
  recent_statuses: ['deployed', 'failed', 'failed', 'failed'],
});

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

  // A flapping app whose most recent task deployed is still Failing. Taking the
  // badge colour from last_status instead of that derived state painted the word
  // "Failing" in the success green.
  it('paints a recovered-but-failing card in the failed colour, not the deployed one', async () => {
    respondWith([flaky]);

    renderPage();

    const chip = await screen.findByText('Failing');
    expect(chip).toHaveStyle({ color: tokens.statusFailedFg });
    expect(chip).not.toHaveStyle({ color: tokens.statusDeployedFg });
  });

  it('paints a recovered-but-failing pinned tile in the failed colour too', async () => {
    respondWith([flaky]);
    renderPage();

    await waitFor(() => expect(screen.getByText('MY APPS')).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: /Pin an app/ }));
    fireEvent.click(await screen.findByRole('option', { name: 'flaky' }));

    const unpin = await screen.findByRole('button', { name: 'Unpin flaky' });
    // Scoped to the tile: the attention card above it also says "Failing".
    const tile = unpin.parentElement as HTMLElement;
    const label = within(tile).getByText('Failing');
    expect(label).toHaveStyle({ color: tokens.statusFailedFg });
    expect(label).not.toHaveStyle({ color: tokens.statusDeployedFg });
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

  // One failure the app has since deployed over is history, not a live problem.
  it('does not card an app that recovered from a single failure', async () => {
    respondWith([
      summary({
        app: 'recovered',
        failed: 1,
        deployed: 3,
        last_status: 'deployed',
        recent_statuses: ['deployed', 'deployed', 'failed', 'deployed'],
      }),
    ]);

    renderPage();

    expect(await screen.findByText('Nothing failing or deploying in this window.')).toBeInTheDocument();
    expect(screen.queryByText('Failing')).toBeNull();
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

  // Recent Tasks only reaches back 24 h, so a 7 d or 30 d card linked there
  // usually opened on "no tasks found". History takes the same range instead.
  describe('links into History over the selected window', () => {
    const NOW_MS = Date.parse('2026-09-07T12:00:00Z');
    const nowSeconds = Math.floor(NOW_MS / 1000);
    const historyHref = (app: string, windowSeconds: number) =>
      `/history?app=${app}&startDate=${nowSeconds - windowSeconds}&endDate=${nowSeconds}`;
    const hrefs = () => screen.getAllByRole('link').map(link => link.getAttribute('href'));

    beforeEach(() => {
      vi.spyOn(Date, 'now').mockReturnValue(NOW_MS);
    });

    afterEach(() => {
      vi.restoreAllMocks();
    });

    it('from an attention card and a list row', async () => {
      respondWith([summary({ app: 'broken', failed: 1, last_status: 'failed' })]);
      renderPage();

      await waitFor(() => expect(screen.getByText('ALL APPLICATIONS')).toBeInTheDocument());
      const expected = historyHref('broken', 24 * 60 * 60);
      // One from the card, one from the all-applications row.
      expect(hrefs().filter(href => href === expected)).toHaveLength(2);
      expect(hrefs().some(href => href?.startsWith('/tasks'))).toBe(false);
    });

    it('from a pinned tile', async () => {
      respondWith([summary({ app: 'checkout' })]);
      renderPage();

      await waitFor(() => expect(screen.getByText('MY APPS')).toBeInTheDocument());
      fireEvent.click(screen.getByRole('button', { name: /Pin an app/ }));
      fireEvent.click(await screen.findByRole('option', { name: 'checkout' }));

      const unpin = await screen.findByRole('button', { name: 'Unpin checkout' });
      const tile = unpin.parentElement as HTMLElement;
      expect(within(tile).getByRole('link')).toHaveAttribute(
        'href',
        historyHref('checkout', 24 * 60 * 60),
      );
    });

    it('follows a window switch', async () => {
      respondWith([summary({ app: 'checkout' })]);
      renderPage();

      await waitFor(() => expect(screen.getByText('ALL APPLICATIONS')).toBeInTheDocument());
      fireEvent.click(screen.getByRole('tab', { name: '30 d' }));

      await waitFor(() =>
        expect(hrefs()).toContain(historyHref('checkout', 30 * 24 * 60 * 60)),
      );
    });

    it('encodes the app name', async () => {
      respondWith([summary({ app: 'team/app one' })]);
      renderPage();

      await waitFor(() => expect(screen.getByText('ALL APPLICATIONS')).toBeInTheDocument());
      expect(hrefs()).toContain(historyHref('team%2Fapp%20one', 24 * 60 * 60));
    });
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
