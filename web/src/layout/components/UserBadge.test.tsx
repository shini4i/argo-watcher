import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { UserBadge } from './UserBadge';

const identityMock = vi.fn();
const logoutMock = vi.fn();

vi.mock('react-admin', async () => {
  const actual = await vi.importActual<typeof import('react-admin')>('react-admin');
  return {
    ...actual,
    useGetIdentity: () => identityMock(),
    useLogout: () => logoutMock,
  };
});

describe('UserBadge', () => {
  beforeEach(() => {
    identityMock.mockReset();
    logoutMock.mockReset();
    identityMock.mockReturnValue({
      identity: { id: 'user-id', fullName: 'Shini4i', email: 'user@example.com' },
      isPending: false,
    });
  });

  it('renders the display name', () => {
    render(<UserBadge privileged={false} />);
    expect(screen.getByText('Shini4i')).toBeInTheDocument();
  });

  it('renders the profile picture when the identity carries one', () => {
    identityMock.mockReturnValue({
      identity: { id: 'user-id', fullName: 'Shini4i', avatar: 'https://idp.example.com/avatar.png' },
      isPending: false,
    });
    const { container } = render(<UserBadge privileged={false} />);

    expect(container.querySelector('img')).toHaveAttribute('src', 'https://idp.example.com/avatar.png');
  });

  it('falls back to the email when no name is present', () => {
    identityMock.mockReturnValue({
      identity: { id: 'user-id', email: 'user@example.com' },
      isPending: false,
    });
    render(<UserBadge privileged={false} />);
    expect(screen.getByText('user@example.com')).toBeInTheDocument();
  });

  it('marks a privileged user with a crown', () => {
    render(<UserBadge privileged />);
    expect(screen.getByRole('img', { name: /privileged access/i })).toBeInTheDocument();
  });

  it('omits the crown for a regular user', () => {
    render(<UserBadge privileged={false} />);
    expect(screen.queryByRole('img', { name: /privileged access/i })).toBeNull();
  });

  it('signs the user out from the badge menu', async () => {
    render(<UserBadge privileged={false} />);
    const user = userEvent.setup();

    await user.click(screen.getByRole('button', { name: /Shini4i/ }));
    await user.click(await screen.findByRole('menuitem', { name: /log out/i }));

    expect(logoutMock).toHaveBeenCalledTimes(1);
  });

  it('closes the menu without signing out when dismissed', async () => {
    render(<UserBadge privileged={false} />);
    const user = userEvent.setup();

    await user.click(screen.getByRole('button', { name: /Shini4i/ }));
    await user.keyboard('{Escape}');

    await waitFor(() => expect(screen.queryByRole('menuitem')).toBeNull());
    expect(logoutMock).not.toHaveBeenCalled();
  });

  it('shows the uppercased initial and no image when the provider serves no picture', () => {
    identityMock.mockReturnValue({
      identity: { id: 'user-id', fullName: 'shini4i', email: 'user@example.com' },
      isPending: false,
    });
    const { container } = render(<UserBadge privileged />);

    expect(screen.getByText('S')).toBeInTheDocument();
    expect(container.querySelector('img')).toBeNull();
  });

  it('shows both the picture and the crown for a privileged user', () => {
    identityMock.mockReturnValue({
      identity: { id: 'user-id', fullName: 'Shini4i', avatar: 'https://idp.example.com/avatar.png' },
      isPending: false,
    });
    const { container } = render(<UserBadge privileged />);

    expect(container.querySelector('img')).toHaveAttribute('src', 'https://idp.example.com/avatar.png');
    expect(screen.getByRole('img', { name: /privileged access/i })).toBeInTheDocument();
  });

  it('falls back to the email as the display name when the name claim is blank', () => {
    identityMock.mockReturnValue({
      identity: { id: 'user-id', fullName: '', email: 'user@example.com' },
      isPending: false,
    });
    render(<UserBadge privileged={false} />);

    expect(screen.getByText('user@example.com')).toBeInTheDocument();
  });

  it('still renders a card with a sign-out menu when the provider names nobody', () => {
    identityMock.mockReturnValue({ identity: { id: 'user-id' }, isPending: false });
    render(<UserBadge privileged={false} />);

    expect(screen.getByRole('button', { name: /Signed in/ })).toBeInTheDocument();
  });

  it('renders nothing when the identity lookup fails', () => {
    identityMock.mockReturnValue({
      identity: undefined,
      isPending: false,
      error: new Error('identity unavailable'),
    });
    const { container } = render(<UserBadge privileged={false} />);

    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing while the identity is loading', () => {
    identityMock.mockReturnValue({ identity: undefined, isPending: true });
    const { container } = render(<UserBadge privileged={false} />);
    expect(container).toBeEmptyDOMElement();
  });
});
