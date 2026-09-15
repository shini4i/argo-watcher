import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';

vi.mock('../../data/httpClient', () => ({
  httpClient: vi.fn(),
}));

import { httpClient } from '../../data/httpClient';
import { resetServerConfigCache } from '../../data/serverConfig';
import { useOidcEnabled } from './useOidcEnabled';

const mockHttpClient = vi.mocked(httpClient);

describe('useOidcEnabled', () => {
  beforeEach(() => {
    mockHttpClient.mockReset();
    // The configuration is cached for the page's lifetime, so without this every case
    // after the first would assert against the first one's response.
    resetServerConfigCache();
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

  it('stays null when the config answers 200 with no JSON body', async () => {
    // The Web UI's own HTML catch-all answers 200. Reading that as "OIDC disabled"
    // would let ConfigDrawer offer the deploy-lock toggle unauthenticated.
    mockHttpClient.mockResolvedValue({ data: undefined, status: 200, headers: new Headers() });

    const { result } = renderHook(() => useOidcEnabled());

    await waitFor(() => expect(mockHttpClient).toHaveBeenCalled());
    expect(result.current).toBeNull();
  });

  it('shares one request with a second consumer of the configuration', async () => {
    mockHttpClient.mockResolvedValue({
      data: { oidc: { enabled: true } },
      status: 200,
      headers: new Headers(),
    });

    const first = renderHook(() => useOidcEnabled());
    const second = renderHook(() => useOidcEnabled());

    await waitFor(() => expect(first.result.current).toBe(true));
    await waitFor(() => expect(second.result.current).toBe(true));
    expect(mockHttpClient).toHaveBeenCalledTimes(1);
  });
});
