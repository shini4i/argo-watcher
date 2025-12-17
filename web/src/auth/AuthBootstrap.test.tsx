import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AuthBootstrap } from './AuthBootstrap';

const mockInitializeAuth = vi.fn();

vi.mock('./authProvider', () => ({
  initializeAuth: () => mockInitializeAuth(),
}));

describe('AuthBootstrap', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows loading state initially', () => {
    mockInitializeAuth.mockReturnValue(new Promise(() => {})); // Never resolves
    render(<AuthBootstrap>{() => <div>App Content</div>}</AuthBootstrap>);

    expect(screen.getByText('Initializing...')).toBeInTheDocument();
  });

  it('renders children when initialization succeeds with Keycloak enabled', async () => {
    mockInitializeAuth.mockResolvedValue({ keycloakEnabled: true });
    render(
      <AuthBootstrap>{keycloakEnabled => <div>Keycloak: {String(keycloakEnabled)}</div>}</AuthBootstrap>,
    );

    await waitFor(() => {
      expect(screen.getByText('Keycloak: true')).toBeInTheDocument();
    });
  });

  it('passes keycloakEnabled=false when Keycloak is disabled', async () => {
    mockInitializeAuth.mockResolvedValue({ keycloakEnabled: false });
    render(
      <AuthBootstrap>{keycloakEnabled => <div>Keycloak: {String(keycloakEnabled)}</div>}</AuthBootstrap>,
    );

    await waitFor(() => {
      expect(screen.getByText('Keycloak: false')).toBeInTheDocument();
    });
  });

  it('shows error state with retry button on failure', async () => {
    mockInitializeAuth.mockRejectedValue(new Error('Network error'));
    render(<AuthBootstrap>{() => <div>App Content</div>}</AuthBootstrap>);

    await waitFor(() => {
      expect(screen.getByText('Network error')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
    });
  });

  it('retries initialization when retry button is clicked', async () => {
    mockInitializeAuth
      .mockRejectedValueOnce(new Error('Network error'))
      .mockResolvedValueOnce({ keycloakEnabled: true });

    render(
      <AuthBootstrap>{keycloakEnabled => <div>Keycloak: {String(keycloakEnabled)}</div>}</AuthBootstrap>,
    );

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('button', { name: /retry/i }));

    await waitFor(() => {
      expect(screen.getByText('Keycloak: true')).toBeInTheDocument();
    });
  });

  it('handles non-Error rejection gracefully', async () => {
    mockInitializeAuth.mockRejectedValue('string error');
    render(<AuthBootstrap>{() => <div>App Content</div>}</AuthBootstrap>);

    await waitFor(() => {
      expect(screen.getByText('Authentication initialization failed')).toBeInTheDocument();
    });
  });
});
