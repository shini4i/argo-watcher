import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { LoginPage } from './LoginPage';

const mockLogin = vi.fn();
const mockNotify = vi.fn();

vi.mock('react-admin', () => ({
  Login: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="login-wrapper">{children}</div>
  ),
  useLogin: () => mockLogin,
  useNotify: () => mockNotify,
}));

describe('LoginPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders login button within Login wrapper', () => {
    render(<LoginPage />);

    expect(screen.getByTestId('login-wrapper')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /login with keycloak/i })).toBeInTheDocument();
    expect(screen.getByText(/sign in to monitor/i)).toBeInTheDocument();
  });

  it('calls login when button is clicked', async () => {
    mockLogin.mockResolvedValue(undefined);
    render(<LoginPage />);

    fireEvent.click(screen.getByRole('button', { name: /login with keycloak/i }));

    await waitFor(() => {
      expect(mockLogin).toHaveBeenCalledWith(
        expect.objectContaining({
          redirectTo: expect.any(String),
        }),
      );
    });
  });

  it('shows error notification on login failure', async () => {
    mockLogin.mockRejectedValue(new Error('Login failed'));
    render(<LoginPage />);

    fireEvent.click(screen.getByRole('button', { name: /login with keycloak/i }));

    await waitFor(() => {
      expect(mockNotify).toHaveBeenCalledWith('Login failed. Please try again.', { type: 'error' });
    });
  });

  it('disables button while loading', async () => {
    mockLogin.mockImplementation(() => new Promise(() => {})); // Never resolves
    render(<LoginPage />);

    const button = screen.getByRole('button', { name: /login with keycloak/i });
    fireEvent.click(button);

    await waitFor(() => {
      expect(button).toBeDisabled();
    });
  });
});
