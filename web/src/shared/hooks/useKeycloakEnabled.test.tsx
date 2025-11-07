import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';

vi.mock('../../data/httpClient', () => ({
  httpClient: vi.fn(),
}));

import { httpClient } from '../../data/httpClient';
import { useKeycloakEnabled } from './useKeycloakEnabled';

const mockHttpClient = vi.mocked(httpClient);

describe('useKeycloakEnabled', () => {
  beforeEach(() => {
    mockHttpClient.mockReset();
  });

  it('returns true when server config enables keycloak', async () => {
    mockHttpClient.mockResolvedValue({
      data: { keycloak: { enabled: true } },
      status: 200,
      headers: new Headers(),
    });

    const { result } = renderHook(() => useKeycloakEnabled());
    await waitFor(() => expect(result.current).toBe(true));
  });

  it('returns false when keycloak is disabled', async () => {
    mockHttpClient.mockResolvedValue({
      data: { keycloak: { enabled: false } },
      status: 200,
      headers: new Headers(),
    });

    const { result } = renderHook(() => useKeycloakEnabled());
    await waitFor(() => expect(result.current).toBe(false));
  });

  it('falls back to false when request fails', async () => {
    mockHttpClient.mockRejectedValue(new Error('network'));

    const { result } = renderHook(() => useKeycloakEnabled());
    await waitFor(() => expect(result.current).toBe(false));
  });
});
