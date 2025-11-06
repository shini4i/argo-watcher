import { render, screen, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { HttpResponse } from '../../data/httpClient';
import { ThemeModeProvider } from '../../theme/ThemeModeProvider';
import { AppTopBar } from './AppTopBar';
import { DeployLockProvider } from '../../features/deployLock/DeployLockProvider';
import { deployLockService } from '../../features/deployLock/deployLockService';

const httpClientMock = vi.fn();
const notifyMock = vi.fn();
const permissionsMock = vi.fn();
const keycloakEnabledMock = vi.fn();

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
    useNotify: () => notifyMock,
    usePermissions: () => permissionsMock(),
  };
});

describe('AppTopBar', () => {
  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    httpClientMock.mockReset();
    notifyMock.mockReset();
    permissionsMock.mockReset();
    keycloakEnabledMock.mockReset();
    keycloakEnabledMock.mockReturnValue(true);
    permissionsMock.mockReturnValue({
      permissions: { groups: ['devops'], privilegedGroups: ['devops'] },
      isLoading: false,
    });
    vi.mocked(deployLockService.subscribe).mockImplementation(listener => {
      listener(false);
      return () => undefined;
    });
  });

  it('displays version and opens config drawer', async () => {
    httpClientMock
      .mockResolvedValueOnce({
        data: '1.2.3',
        status: 200,
        headers: {} as HttpResponse<unknown>['headers'],
      })
      .mockResolvedValueOnce({
        data: { foo: 'bar' },
        status: 200,
        headers: {} as HttpResponse<unknown>['headers'],
      });

    render(
      <ThemeModeProvider>
        <DeployLockProvider>
          <MemoryRouter>
            <AppTopBar open title="Argo Watcher" />
          </MemoryRouter>
        </DeployLockProvider>
      </ThemeModeProvider>,
    );

    await screen.findByText('1.2.3');

    const user = userEvent.setup();
    await act(async () => {
      await user.click(screen.getByLabelText(/open configuration drawer/i));
    });

    await waitFor(() => {
      expect(httpClientMock).toHaveBeenCalledTimes(2);
    });
    await screen.findByText(/foo/i);
  });
});
vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
