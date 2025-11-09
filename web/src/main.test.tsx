import { render, screen } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

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
    document.body.innerHTML = '';
  });

  it('throws when the root element is missing', async () => {
    await expect(import('./main')).rejects.toThrow(
      'Root element was not found. Ensure index.html contains a div with id="root".',
    );
    expect(createRootMock).not.toHaveBeenCalled();
  });

  it('creates the root and renders the provider tree', async () => {
    document.body.innerHTML = '<div id="root"></div>';

    await import('./main');

    const rootElement = document.getElementById('root');
    expect(createRootMock).toHaveBeenCalledWith(rootElement);
    expect(renderMock).toHaveBeenCalledTimes(1);

    const renderedTree = renderMock.mock.calls[0][0] as ReactElement;
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
