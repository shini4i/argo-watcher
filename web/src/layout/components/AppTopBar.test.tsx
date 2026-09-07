import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { HttpResponse } from '../../data/httpClient';
import { ThemeModeProvider } from '../../theme/ThemeModeProvider';
import { AppTopBar } from './AppTopBar';
import { DeployLockProvider } from '../../features/deployLock/DeployLockProvider';
import { deployLockService } from '../../features/deployLock/deployLockService';

// Flag the environment as act-aware before tests run to suppress React warnings during async renders.
vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);

const httpClientMock = vi.fn();
const notifyMock = vi.fn();
const permissionsMock = vi.fn();
const oidcEnabledMock = vi.fn();
const identityMock = vi.fn();

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

vi.mock('../../shared/hooks/useOidcEnabled', () => ({
  useOidcEnabled: () => oidcEnabledMock(),
}));

vi.mock('../../shared/hooks/useAppTokensAvailable', () => ({
  useAppTokensAvailable: () => false,
}));

vi.mock('react-admin', async () => {
  const actual = await vi.importActual<typeof import('react-admin')>('react-admin');
  return {
    ...actual,
    useNotify: () => notifyMock,
    usePermissions: () => permissionsMock(),
    useGetIdentity: () => identityMock(),
    useLogout: () => vi.fn(),
  };
});

describe('AppTopBar', () => {
  beforeEach(() => {
    httpClientMock.mockReset();
    notifyMock.mockReset();
    permissionsMock.mockReset();
    oidcEnabledMock.mockReset();
    oidcEnabledMock.mockReturnValue(true);
    identityMock.mockReset();
    identityMock.mockReturnValue({
      identity: { id: 'user-id', fullName: 'Shini4i' },
      isPending: false,
    });
    permissionsMock.mockReturnValue({
      permissions: { groups: ['devops'], privilegedGroups: ['devops'] },
      isLoading: false,
    });
    vi.mocked(deployLockService.subscribe).mockImplementation(listener => {
      listener(false);
      return () => undefined;
    });
  });

  // The tasks Resource settles the URL on /tasks, so keying Recent on an exact
  // `/` left it lit only for the instant before react-admin normalized the path.
  describe('navigation highlight', () => {
    const renderAt = (path: string) =>
      render(
        <ThemeModeProvider>
          <DeployLockProvider>
            <MemoryRouter initialEntries={[path]}>
              <AppTopBar open title="Argo Watcher" />
            </MemoryRouter>
          </DeployLockProvider>
        </ThemeModeProvider>,
      );

    // MUI renders `color="secondary"` as a class, which is what the eye reads as
    // "this is the screen you are on".
    const isHighlighted = (label: string) =>
      screen.getByLabelText(label).className.includes('colorSecondary');

    it.each(['/', '/tasks'])('marks Recent as current at %s', path => {
      renderAt(path);

      expect(isHighlighted('Recent')).toBe(true);
      expect(isHighlighted('Overview')).toBe(false);
      expect(isHighlighted('History')).toBe(false);
    });

    it('marks Overview as current, and leaves Recent unlit', () => {
      renderAt('/overview');

      expect(isHighlighted('Overview')).toBe(true);
      expect(isHighlighted('Recent')).toBe(false);
    });

    // The detail page is not the list, and lighting Recent there would claim it is.
    it('marks nothing as current on a task detail page', () => {
      renderAt('/task/abc-123');

      expect(isHighlighted('Recent')).toBe(false);
      expect(isHighlighted('Overview')).toBe(false);
      expect(isHighlighted('History')).toBe(false);
    });
  });

  it('displays version and opens config drawer', async () => {
    httpClientMock.mockResolvedValueOnce({
      data: '1.2.3',
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
    await user.click(screen.getByLabelText(/open configuration drawer/i));

    await waitFor(() => {
      expect(screen.getByLabelText(/Workspace configuration drawer/i)).toBeInTheDocument();
    });
  });
});
