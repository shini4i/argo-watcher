import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { HttpResponse } from '../../data/httpClient';
import { ThemeModeProvider, useThemeMode } from '../../theme/ThemeModeProvider';
import { ConfigDrawer } from './ConfigDrawer';
import { deployLockService } from '../../features/deployLock/deployLockService';
import { DeployLockProvider } from '../../features/deployLock/DeployLockProvider';
import { TimezoneProvider } from '../../shared/providers/TimezoneProvider';

vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);

const notifyMock = vi.fn();
const keycloakEnabledMock = vi.fn();
const permissionsMock = vi.fn();

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
      <TimezoneProvider>
        <DeployLockProvider>
          <ThemeModeConsumer />
          <ConfigDrawer open onClose={() => undefined} version="1.0.0" />
        </DeployLockProvider>
      </TimezoneProvider>
    </ThemeModeProvider>,
  );

describe('ConfigDrawer', () => {
  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
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

    await screen.findByRole('switch', { name: /toggle deploy lock/i });
    const toggles = screen.getAllByRole('switch', { name: /toggle deploy lock/i });
    expect(toggles).toHaveLength(1);
    expect(toggles[0]).toBeDisabled();
  });

  it('hides the deploy lock toggle when Keycloak is disabled', async () => {
    keycloakEnabledMock.mockReturnValue(false);
    renderDrawer();

    expect(screen.queryByRole('switch', { name: /toggle deploy lock/i })).toBeNull();
    expect(screen.getByText(/manual deploy lock requires keycloak/i)).toBeInTheDocument();
  });

  it('hides the deploy lock toggle while Keycloak status is unknown', async () => {
    // Default-deny during the config-loading / request-failed window: the toggle
    // must not be rendered until Keycloak status is known.
    keycloakEnabledMock.mockReturnValue(null);
    renderDrawer();

    expect(screen.queryByRole('switch', { name: /toggle deploy lock/i })).toBeNull();
    expect(screen.getByText(/checking permissions/i)).toBeInTheDocument();
  });

  it('calls deploy lock service when toggled', async () => {
    renderDrawer();
    await screen.findByRole('switch', { name: /toggle deploy lock/i });

    const user = userEvent.setup();
    await act(async () => {
      await user.click(screen.getByRole('switch', { name: /toggle deploy lock/i }));
    });
    expect(deployLockService.setLock).toHaveBeenCalled();
  });

  it('updates timezone preference when toggled', async () => {
    renderDrawer();
    const user = userEvent.setup();
    await act(async () => {
      await user.click(await screen.findByRole('button', { name: /Local/ }));
    });

    expect(globalThis.localStorage?.getItem('argo-watcher:timezone')).toBe('local');
  });

  it('releases deploy lock when already engaged', async () => {
    vi.mocked(deployLockService.subscribe).mockImplementation(listener => {
      listener(true);
      return () => undefined;
    });
    renderDrawer();
    const user = userEvent.setup();
    await act(async () => {
      await user.click(await screen.findByRole('switch', { name: /toggle deploy lock/i }));
    });

    expect(deployLockService.releaseLock).toHaveBeenCalled();
    expect(notifyMock).toHaveBeenCalledWith('Deploy lock released.', { type: 'info' });
  });

  it('does not render the backend configuration section', () => {
    renderDrawer();
    expect(screen.queryByText(/Backend Configuration/i)).toBeNull();
    expect(screen.queryByRole('button', { name: /copy configuration/i })).toBeNull();
  });
});
