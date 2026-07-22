import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';

vi.mock('../../data/httpClient', () => ({
  httpClient: vi.fn(),
}));

import { httpClient } from '../../data/httpClient';
import { useOidcEnabled } from './useOidcEnabled';

const mockHttpClient = vi.mocked(httpClient);

describe('useOidcEnabled', () => {
  beforeEach(() => {
    mockHttpClient.mockReset();
  });

  it('returns true when server config enables oidc', async () => {
    mockHttpClient.mockResolvedValue({
      data: { oidc: { enabled: true } },
      status: 200,
      headers: new Headers(),
    });

    const { result } = renderHook(() => useOidcEnabled());
    await waitFor(() => expect(result.current).toBe(true));
  });

  it('returns false when oidc is disabled', async () => {
    mockHttpClient.mockResolvedValue({
      data: { oidc: { enabled: false } },
      status: 200,
      headers: new Headers(),
    });

    const { result } = renderHook(() => useOidcEnabled());
    await waitFor(() => expect(result.current).toBe(false));
  });

  it('stays null when the request fails so callers can default-deny', async () => {
    mockHttpClient.mockRejectedValue(new Error('network'));

    const { result } = renderHook(() => useOidcEnabled());
    // Wait one tick so the catch handler has a chance to run.
    await new Promise(resolve => setTimeout(resolve, 0));
    expect(result.current).toBeNull();
  });
});
