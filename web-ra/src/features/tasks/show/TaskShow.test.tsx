import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { TaskStatus } from '../../../data/types';
import { TaskShow } from './TaskShow';

const mockUseGetOne = vi.fn();
const mockUseNotify = vi.fn();
const mockUsePermissions = vi.fn();
const mockUseGetIdentity = vi.fn();
const mockUseDeployLockState = vi.fn();
const mockUseKeycloakEnabled = vi.fn();
const mockHttpClient = vi.fn();
const mockGetAccessToken = vi.fn();

vi.mock('react-admin', async () => {
  const actual = await vi.importActual<typeof import('react-admin')>('react-admin');
  return {
    ...actual,
    useGetOne: (resource: string, params: { id: string }, options?: unknown) =>
      mockUseGetOne(resource, params, options),
    useNotify: () => mockUseNotify,
    usePermissions: () => mockUsePermissions(),
    useGetIdentity: () => mockUseGetIdentity(),
  };
});

vi.mock('../../deployLock/useDeployLockState', () => ({
  useDeployLockState: () => mockUseDeployLockState(),
}));

vi.mock('../../../shared/hooks/useKeycloakEnabled', () => ({
  useKeycloakEnabled: () => mockUseKeycloakEnabled(),
}));

vi.mock('../../../data/httpClient', () => ({
  httpClient: (...args: unknown[]) => mockHttpClient(...args),
}));

vi.mock('../../../auth/tokenStore', () => ({
  getAccessToken: () => mockGetAccessToken(),
}));

const renderWithRouter = (path: string) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/task/:id" element={<TaskShow />} />
        <Route path="/" element={<div data-testid="home-route" />} />
      </Routes>
    </MemoryRouter>,
  );

const buildTask = (overrides: Partial<TaskStatus> = {}): TaskStatus => ({
  id: 'task-1',
  app: 'demo-app',
  author: 'alice',
  project: 'demo',
  created: 1690000000,
  updated: 1690003600,
  status: 'deployed',
  status_reason: 'All good.',
  images: [{ image: 'registry/demo', tag: 'v1' }],
  ...overrides,
});

describe('TaskShow', () => {
  beforeEach(() => {
    mockUseGetOne.mockReset();
    mockUseNotify.mockClear();
    mockUsePermissions.mockReturnValue({
      permissions: { groups: ['devops'], privilegedGroups: ['devops'] },
      isLoading: false,
    });
    mockUseGetIdentity.mockReturnValue({
      data: { id: 'user-1', email: 'user@example.com' },
      isLoading: false,
    });
    mockUseDeployLockState.mockReturnValue(false);
    mockUseKeycloakEnabled.mockReturnValue(true);
    mockHttpClient.mockReset();
    mockHttpClient.mockResolvedValue({ data: {}, status: 202, headers: {} as Headers });
    mockGetAccessToken.mockReturnValue('token');
  });

  it('renders basic task summary when data resolves', () => {
    const refetch = vi.fn();
    mockUseGetOne.mockReturnValue({
      data: buildTask(),
      isLoading: false,
      isError: false,
      refetch,
    });

    renderWithRouter('/task/task-1');

    expect(screen.getByText('demo-app')).toBeInTheDocument();
    expect(screen.getByText(/Task ID/i)).toBeInTheDocument();
    expect(screen.getByText('task-1')).toBeInTheDocument();
    expect(screen.getByText('Images')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Refresh/i })).toBeInTheDocument();
  });

  it('invokes refetch when Refresh button clicked', () => {
    const refetch = vi.fn();
    mockUseGetOne.mockReturnValue({
      data: buildTask(),
      isLoading: false,
      isError: false,
      refetch,
    });

    renderWithRouter('/task/task-1');
    fireEvent.click(screen.getByRole('button', { name: /Refresh/i }));

    expect(refetch).toHaveBeenCalled();
  });

  it('shows fallback when identifier missing', () => {
    mockUseGetOne.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter initialEntries={['/task']}>
        <Routes>
          <Route path="/task" element={<TaskShow />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByText(/Task not specified/i)).toBeInTheDocument();
  });

  it('automatically refetches every 10 seconds while task is in progress', () => {
    vi.useFakeTimers();
    try {
      const refetch = vi.fn();
      mockUseGetOne.mockReturnValue({
        data: buildTask({ status: 'in progress' }),
        isLoading: false,
        isError: false,
        refetch,
      });

      renderWithRouter('/task/task-1');

      vi.advanceTimersByTime(10_000);
      expect(refetch).toHaveBeenCalledTimes(1);
      vi.advanceTimersByTime(10_000);
      expect(refetch).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it('triggers rollback request when confirmed', async () => {
    const refetch = vi.fn();
    mockUseGetOne.mockReturnValue({
      data: buildTask({ status: 'deployed' }),
      isLoading: false,
      isError: false,
      refetch,
    });

    renderWithRouter('/task/task-1');

    fireEvent.click(screen.getByRole('button', { name: /Rollback to this version/i }));
    fireEvent.click(screen.getByRole('button', { name: /^Yes$/i }));

    await waitFor(() => {
      expect(mockHttpClient).toHaveBeenCalledTimes(1);
    });

    const [, options] = mockHttpClient.mock.calls[0];
    expect(options).toMatchObject({
      method: 'POST',
    });
    expect((options as { body: Record<string, unknown> }).body).toMatchObject({
      author: 'user@example.com',
    });
    expect((options as { headers?: Record<string, string> }).headers).toMatchObject({
      'Keycloak-Authorization': 'Bearer token',
    });
  });

  it('disables rollback button when deploy lock active', () => {
    mockUseDeployLockState.mockReturnValue(true);
    mockUseGetOne.mockReturnValue({
      data: buildTask({ status: 'deployed' }),
      isLoading: false,
      isError: false,
      refetch: vi.fn(),
    });

    renderWithRouter('/task/task-1');

    expect(screen.getByRole('button', { name: /Rollback to this version/i })).toBeDisabled();
  });
});
