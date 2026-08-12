import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactElement, ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { AuthFailure } from './auth/authFailure';

let resolveBootstrap: (failure?: AuthFailure | null) => void = () => {};
let notifyOidcEnabled: () => void = () => {};
const bootstrapAuthMock = vi.fn(
  (options?: { onOidcEnabled?: () => void }) =>
    new Promise<AuthFailure | null>(resolve => {
      resolveBootstrap = (failure = null) => resolve(failure);
      notifyOidcEnabled = () => options?.onOidcEnabled?.();
    }),
);

vi.mock('./auth/authProvider', () => ({
  bootstrapAuth: bootstrapAuthMock,
  authProvider: {},
}));

const AppSplashStub = ({
  message,
  error,
  onRetry,
}: {
  message: string;
  error?: AuthFailure;
  onRetry?: () => void;
}) => (
  <div data-testid="app-splash">
    {message}
    {error && <div data-testid="splash-error">{error.title}</div>}
    {onRetry && <button data-testid="splash-retry" onClick={onRetry} type="button" />}
  </div>
);

vi.mock('./layout/AppSplash', () => ({
  AppSplash: AppSplashStub,
}));

const renderMock = vi.fn();
const createRootMock = vi.fn(() => ({ render: renderMock }));

vi.mock('react-dom/client', () => ({
  default: {
    createRoot: createRootMock,
  },
}));

const BrowserRouterStub = ({ children }: { children: ReactNode }) => (
  <div data-testid="browser-router">{children}</div>
);

vi.mock('react-router-dom', () => ({
  BrowserRouter: BrowserRouterStub,
}));

const AppProvidersStub = ({ children }: { children: ReactNode }) => (
  <div data-testid="app-providers">{children}</div>
);

vi.mock('./shared/providers/AppProviders', () => ({
  AppProviders: AppProvidersStub,
}));

const AppStub = () => <div data-testid="app-component" />;

vi.mock('./App', () => ({
  App: AppStub,
}));

describe('main entrypoint', () => {
  beforeEach(() => {
    vi.resetModules();
    renderMock.mockClear();
    createRootMock.mockClear();
    bootstrapAuthMock.mockClear();
    document.body.innerHTML = '';
  });

  it('throws when the root element is missing', async () => {
    await expect(import('./main')).rejects.toThrow(
      'Root element was not found. Ensure index.html contains a div with id="root".',
    );
    expect(createRootMock).not.toHaveBeenCalled();
  });

  it('renders the loading screen before authentication resolves', async () => {
    document.body.innerHTML = '<div id="root"></div>';

    await import('./main');

    expect(renderMock).toHaveBeenCalledTimes(1);

    const splashTree = renderMock.mock.calls[0][0] as ReactElement;
    const { unmount } = render(splashTree);

    // Neutral copy: whether a sign-in is involved is not known until the server
    // config answers, so an auth-less deployment must not be told it is signing in.
    expect(screen.getByTestId('app-splash')).toHaveTextContent('Loading…');
    // The splash must not pull in AppProviders: the deploy-lock and ArgoCD-status
    // providers start polling the API on mount, before any token exists.
    expect(screen.queryByTestId('app-providers')).toBeNull();
    expect(screen.queryByTestId('browser-router')).toBeNull();
    expect(screen.queryByTestId('app-component')).toBeNull();

    unmount();
  });

  it('narrows the status line once the config confirms OIDC is enabled', async () => {
    document.body.innerHTML = '<div id="root"></div>';

    await import('./main');

    notifyOidcEnabled();
    await waitFor(() => expect(renderMock).toHaveBeenCalledTimes(2));

    const secondTree = renderMock.mock.calls[1][0] as ReactElement;
    const { unmount } = render(secondTree);

    expect(screen.getByTestId('app-splash')).toHaveTextContent('Signing in…');
    // Still the splash — the app tree waits for the bootstrap to settle.
    expect(screen.queryByTestId('app-component')).toBeNull();

    unmount();
  });

  it('replaces the sign-in splash with the app once the bootstrap settles', async () => {
    document.body.innerHTML = '<div id="root"></div>';

    await import('./main');

    notifyOidcEnabled();
    resolveBootstrap();
    await waitFor(() => expect(renderMock).toHaveBeenCalledTimes(3));

    // The splash render must land before the app render, or a signed-in user is
    // left staring at the loading screen.
    const splash = render(renderMock.mock.calls[1][0] as ReactElement);
    expect(screen.getByTestId('app-splash')).toHaveTextContent('Signing in…');
    splash.unmount();

    const app = render(renderMock.mock.calls[2][0] as ReactElement);
    expect(screen.getByTestId('app-component')).toBeInTheDocument();
    expect(screen.queryByTestId('app-splash')).toBeNull();
    app.unmount();
  });

  it('keeps the neutral status line when OIDC is disabled', async () => {
    document.body.innerHTML = '<div id="root"></div>';

    await import('./main');

    // Auth-less deployments never get the callback; the bootstrap just settles.
    resolveBootstrap();
    await waitFor(() => expect(renderMock).toHaveBeenCalledTimes(2));

    // Exactly two renders — neutral splash, then the app. A "Signing in…" render
    // must never appear in between.
    expect(renderMock.mock.calls).toHaveLength(2);

    const splash = render(renderMock.mock.calls[0][0] as ReactElement);
    expect(screen.getByTestId('app-splash')).toHaveTextContent('Loading…');
    expect(screen.getByTestId('app-splash')).not.toHaveTextContent('Signing in');
    splash.unmount();

    const app = render(renderMock.mock.calls[1][0] as ReactElement);
    expect(screen.getByTestId('app-component')).toBeInTheDocument();
    app.unmount();
  });

  it('shows the failure on the splash instead of mounting the app', async () => {
    document.body.innerHTML = '<div id="root"></div>';

    await import('./main');

    notifyOidcEnabled();
    resolveBootstrap({
      kind: 'redirect_failed',
      title: 'Could not start the sign-in',
      detail: 'Failed to fetch',
    });
    await waitFor(() => expect(renderMock).toHaveBeenCalledTimes(3));

    const { unmount } = render(renderMock.mock.calls[2][0] as ReactElement);

    expect(screen.getByTestId('splash-error')).toHaveTextContent('Could not start the sign-in');
    // The status line still states what happened, next to the box saying why.
    expect(screen.getByTestId('app-splash')).toHaveTextContent('Sign-in failed');
    // Mounting react-admin here is what produced the silent, session-less app: it
    // immediately re-runs checkAuth and heads back to the broken provider.
    expect(screen.queryByTestId('app-component')).toBeNull();

    unmount();
  });

  it('retries by reloading, and only when the user asks', async () => {
    document.body.innerHTML = '<div id="root"></div>';
    const reload = vi.fn();
    // jsdom does not implement navigation, so the real reload would only log an
    // error. Restored below so later tests keep the genuine location.
    const realLocation = window.location;
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...realLocation, reload },
    });

    try {
      await import('./main');

      // Nothing to retry while the sign-in is still in flight.
      const loading = render(renderMock.mock.calls[0][0] as ReactElement);
      expect(screen.queryByTestId('splash-retry')).toBeNull();
      loading.unmount();

      resolveBootstrap({ kind: 'callback_failed', title: 'The sign-in response failed' });
      await waitFor(() => expect(renderMock).toHaveBeenCalledTimes(2));

      const { unmount } = render(renderMock.mock.calls[1][0] as ReactElement);
      expect(reload).not.toHaveBeenCalled();

      // The reload re-runs the whole bootstrap — the only escape from the screen.
      await userEvent.click(screen.getByTestId('splash-retry'));
      expect(reload).toHaveBeenCalledTimes(1);

      unmount();
    } finally {
      Object.defineProperty(window, 'location', { configurable: true, value: realLocation });
    }
  });

  it('still renders the app when the auth bootstrap rejects', async () => {
    document.body.innerHTML = '<div id="root"></div>';
    bootstrapAuthMock.mockReturnValueOnce(Promise.reject(new Error('bootstrap failed')));

    await import('./main');

    // A rejected bootstrap must not leave the user stuck on the loading screen.
    await waitFor(() => expect(renderMock).toHaveBeenCalledTimes(2));

    const renderedTree = renderMock.mock.calls[1][0] as ReactElement;
    const { unmount } = render(renderedTree);

    expect(screen.getByTestId('app-component')).toBeInTheDocument();

    unmount();
  });

  it('creates the root and renders the provider tree', async () => {
    document.body.innerHTML = '<div id="root"></div>';

    await import('./main');

    const rootElement = document.getElementById('root');
    expect(createRootMock).toHaveBeenCalledWith(rootElement);

    resolveBootstrap();
    await waitFor(() => expect(renderMock).toHaveBeenCalledTimes(2));

    const renderedTree = renderMock.mock.calls[1][0] as ReactElement;
    const { unmount } = render(renderedTree);

    expect(screen.getByTestId('browser-router')).toContainElement(
      screen.getByTestId('app-providers'),
    );
    expect(screen.getByTestId('app-providers')).toContainElement(
      screen.getByTestId('app-component'),
    );

    unmount();
  });
});
