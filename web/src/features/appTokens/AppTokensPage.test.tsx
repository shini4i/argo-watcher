import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AppTokensPage } from './AppTokensPage';
import type { AppToken } from './appTokensService';

vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);

const notifyMock = vi.fn();
const availableMock = vi.fn();
const permissionsMock = vi.fn();
const listMock = vi.fn();
const issueMock = vi.fn();
const revokeMock = vi.fn();

vi.mock('./appTokensService', async () => {
  const actual = await vi.importActual<typeof import('./appTokensService')>('./appTokensService');
  return {
    ...actual,
    listAppTokens: () => listMock(),
    issueAppToken: (request: unknown) => issueMock(request),
    revokeAppToken: (id: string) => revokeMock(id),
  };
});

vi.mock('../../shared/hooks/useAppTokensAvailable', () => ({
  useAppTokensAvailable: () => availableMock(),
}));

vi.mock('react-admin', async () => {
  const actual = await vi.importActual<typeof import('react-admin')>('react-admin');
  return {
    ...actual,
    usePermissions: () => permissionsMock(),
    useNotify: () => notifyMock,
  };
});

const token = (overrides: Partial<AppToken> = {}): AppToken => ({
  id: 'a1',
  all_apps: false,
  apps: ['app1'],
  hint: '3f9a',
  description: 'ci pipeline',
  created_by: 'alice',
  created_at: 1_700_000_000_000,
  ...overrides,
});

const privileged = () => ({ permissions: { groups: ['admins'], privilegedGroups: ['admins'] } });

describe('AppTokensPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    availableMock.mockReturnValue(true);
    permissionsMock.mockReturnValue(privileged());
    listMock.mockResolvedValue([]);
  });

  describe('access', () => {
    it.each([
      ['an unprivileged user', true, { permissions: { groups: ['devs'], privilegedGroups: ['admins'] } }],
      ['a server without the feature', false, privileged()],
      ['an unresolved availability', null, privileged()],
    ])('refuses %s', async (_name, oidc, permissions) => {
      availableMock.mockReturnValue(oidc);
      permissionsMock.mockReturnValue(permissions);

      render(<AppTokensPage />);

      expect(
        await screen.findByText(/require authentication, the Postgres state backend, and privileged/i),
      ).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: 'Issue token' })).not.toBeInTheDocument();
    });
  });

  it('lists the issued tokens without ever showing a secret', async () => {
    listMock.mockResolvedValue([
      token(),
      token({ id: 'a2', all_apps: true, apps: [], hint: 'bb01', description: '' }),
      token({ id: 'a3', hint: 'cc02', revoked_at: 1_700_000_100_000 }),
    ]);

    render(<AppTokensPage />);

    expect(await screen.findByText('awt_…3f9a')).toBeInTheDocument();
    expect(screen.getByText('All applications')).toBeInTheDocument();
    expect(screen.getAllByText('app1')).toHaveLength(2);
    expect(screen.getByText('Revoked')).toBeInTheDocument();
    expect(screen.getAllByText('Active')).toHaveLength(2);

    // A revoked token cannot be revoked again, so it carries no action.
    expect(screen.getAllByRole('button', { name: 'Revoke' })).toHaveLength(2);
  });

  it('says so when nothing has been issued', async () => {
    render(<AppTokensPage />);

    expect(await screen.findByText(/No deploy tokens have been issued/i)).toBeInTheDocument();
  });

  it('surfaces a failure to load', async () => {
    listMock.mockRejectedValue(new Error('unauthorized'));

    render(<AppTokensPage />);

    expect(await screen.findByText('unauthorized')).toBeInTheDocument();
  });

  it('shows a freshly issued secret once, then forgets it', async () => {
    const user = userEvent.setup();
    issueMock.mockResolvedValue(token({ secret: 'awt_brandNewSecret' }));

    render(<AppTokensPage />);

    await user.click(await screen.findByRole('button', { name: 'Issue token' }));
    await user.type(screen.getByLabelText(/^Applications/), 'app1');
    await user.click(screen.getByRole('button', { name: 'Issue' }));

    const secretField = await screen.findByLabelText('Deploy token secret');
    expect(secretField).toHaveValue('awt_brandNewSecret');
    expect(issueMock).toHaveBeenCalledWith(
      expect.objectContaining({ apps: ['app1'] }),
    );

    await user.click(screen.getByRole('button', { name: 'Done' }));

    await waitFor(() => {
      expect(screen.queryByLabelText('Deploy token secret')).not.toBeInTheDocument();
    });
  });

  it('revokes a token and reloads the list', async () => {
    const user = userEvent.setup();
    listMock.mockResolvedValue([token()]);
    revokeMock.mockResolvedValue(undefined);

    render(<AppTokensPage />);

    await user.click(await screen.findByRole('button', { name: 'Revoke' }));

    await waitFor(() => {
      expect(revokeMock).toHaveBeenCalledWith('a1');
    });
    // Two loads: the initial one and the reload after the mutation, so the table
    // shows what the server holds rather than an optimistic guess.
    expect(listMock).toHaveBeenCalledTimes(2);
  });

  it('reports a failed revocation without dropping the row', async () => {
    const user = userEvent.setup();
    listMock.mockResolvedValue([token()]);
    revokeMock.mockRejectedValue(new Error('token is gone'));

    render(<AppTokensPage />);

    await user.click(await screen.findByRole('button', { name: 'Revoke' }));

    await waitFor(() => {
      expect(notifyMock).toHaveBeenCalledWith('token is gone', { type: 'error' });
    });
    expect(screen.getByText('awt_…3f9a')).toBeInTheDocument();
  });
});
