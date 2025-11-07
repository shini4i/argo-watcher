import { render, screen, act, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { HttpResponse } from '../../data/httpClient';
import { ThemeModeProvider, useThemeMode } from '../../theme/ThemeModeProvider';
import { ConfigDrawer } from './ConfigDrawer';
import { deployLockService } from '../../features/deployLock/deployLockService';
import { DeployLockProvider } from '../../features/deployLock/DeployLockProvider';

vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);

const httpClientMock = vi.fn();
const notifyMock = vi.fn();
const keycloakEnabledMock = vi.fn();
const permissionsMock = vi.fn();

vi.mock('../../data/httpClient', () => ({
  httpClient: (...args: unknown[]) => httpClientMock(...args),
}));

vi.mock('../../features/deployLock/deployLockService', () => ({
  deployLockService: {
    setLock: vi.fn(),
    releaseLock: vi.fn(),
    subscribe: vi.fn(),
  },
}));

vi.mock('../../shared/hooks/useKeycloakEnabled', () => ({
  useKeycloakEnabled: () => keycloakEnabledMock(),
}));

vi.mock('react-admin', async () => {
  const actual = await vi.importActual<typeof import('react-admin')>('react-admin');
  return {
    ...actual,
    usePermissions: () => permissionsMock(),
    useNotify: () => notifyMock,
  };
});

const ThemeModeConsumer = () => {
  const { mode } = useThemeMode();
  return <span data-testid="theme-mode">{mode}</span>;
};

const renderDrawer = () =>
  render(
    <ThemeModeProvider>
      <DeployLockProvider>
        <ThemeModeConsumer />
        <ConfigDrawer open onClose={() => undefined} version="1.0.0" />
      </DeployLockProvider>
    </ThemeModeProvider>,
  );

describe('ConfigDrawer', () => {
  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    httpClientMock.mockReset();
    notifyMock.mockReset();
    vi.mocked(deployLockService.setLock).mockReset();
    vi.mocked(deployLockService.releaseLock).mockReset();
    vi.mocked(deployLockService.subscribe).mockReset();
    keycloakEnabledMock.mockReset();
    permissionsMock.mockReset();
    keycloakEnabledMock.mockReturnValue(true);
    permissionsMock.mockReturnValue({
      permissions: { groups: ['devops'], privilegedGroups: ['devops'] },
      isLoading: false,
    });
    vi.mocked(deployLockService.subscribe).mockImplementation(listener => {
      listener(false);
      return () => undefined;
    });
    vi.mocked(deployLockService.setLock).mockResolvedValue({
      data: {},
      status: 200,
      headers: {} as HttpResponse<unknown>['headers'],
    });
    vi.mocked(deployLockService.releaseLock).mockResolvedValue({
      data: {},
      status: 200,
      headers: {} as HttpResponse<unknown>['headers'],
    });
    httpClientMock.mockResolvedValue({
      data: { foo: 'bar' },
      status: 200,
      headers: {} as HttpResponse<unknown>['headers'],
    });
  });

  it('renders configuration values and copies JSON to clipboard', async () => {
    const writeText = vi.fn();
    Object.defineProperty(window.navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    });

    renderDrawer();

    await screen.findByText(/foo/i);

    const user = userEvent.setup();
    await act(async () => {
      await user.click(screen.getByRole('button', { name: /copy configuration/i }));
    });

    await waitFor(() =>
      expect(notifyMock).toHaveBeenCalledWith('Configuration copied to clipboard.', { type: 'info' }),
    );
  });

  it('toggles theme mode', async () => {
    renderDrawer();
    const user = userEvent.setup();
    await act(async () => {
      const toggleButton = await screen.findByRole('button', { name: /Switch to dark/i });
      await user.click(toggleButton);
    });
    expect(screen.getByTestId('theme-mode').textContent).toBe('dark');
  });

  it('disables deploy lock switch when user not privileged', async () => {
    permissionsMock.mockReturnValue({
      permissions: { groups: ['dev'], privilegedGroups: ['admins'] },
      isLoading: false,
    });
    renderDrawer();

    await screen.findByRole('checkbox', { name: /toggle deploy lock/i });
    const toggles = screen.getAllByRole('checkbox', { name: /toggle deploy lock/i });
    expect(toggles).toHaveLength(1);
    expect(toggles[0]).toBeDisabled();
  });

  it('calls deploy lock service when toggled', async () => {
    renderDrawer();
    await screen.findByRole('checkbox', { name: /toggle deploy lock/i });

    const user = userEvent.setup();
    await act(async () => {
      await user.click(screen.getByRole('checkbox', { name: /toggle deploy lock/i }));
    });
    expect(deployLockService.setLock).toHaveBeenCalled();
  });
});
