import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';

vi.mock('./httpClient', () => ({
  httpClient: vi.fn(),
}));

import { httpClient } from './httpClient';
import { fetchServerConfig, resetServerConfigCache, useServerConfig } from './serverConfig';

const mockHttpClient = vi.mocked(httpClient);

const respondWith = (data: unknown) => ({ data, status: 200, headers: new Headers() });

describe('fetchServerConfig', () => {
  beforeEach(() => {
    mockHttpClient.mockReset();
    resetServerConfigCache();
  });

  it('answers every caller from one request', async () => {
    mockHttpClient.mockResolvedValue(respondWith({ oidc: { enabled: true } }));

    const [first, second, third] = await Promise.all([
      fetchServerConfig(),
      fetchServerConfig(),
      fetchServerConfig(),
    ]);

    expect(mockHttpClient).toHaveBeenCalledTimes(1);
    expect(first).toEqual({ oidc: { enabled: true } });
    expect(second).toBe(first);
    expect(third).toBe(first);
  });

  it('serves a later caller from the cache', async () => {
    mockHttpClient.mockResolvedValue(respondWith({ oidc: { enabled: true }, state_type: 'postgres' }));

    await fetchServerConfig();
    await fetchServerConfig();

    expect(mockHttpClient).toHaveBeenCalledTimes(1);
  });

  it('retries after a failure instead of caching it', async () => {
    mockHttpClient.mockRejectedValueOnce(new Error('network'));
    mockHttpClient.mockResolvedValueOnce(respondWith({ oidc: { enabled: false } }));

    await expect(fetchServerConfig()).rejects.toThrow('network');
    await expect(fetchServerConfig()).resolves.toEqual({ oidc: { enabled: false } });
    expect(mockHttpClient).toHaveBeenCalledTimes(2);
  });

  it.each([
    ['no JSON body at all', undefined],
    ['an array', []],
    ['an error envelope with no oidc', { error: 'gateway timeout' }],
    ['an oidc value that is not an object', { oidc: 'unexpected' }],
    ['a null oidc', { oidc: null }],
  ])('rejects %s rather than caching it as a configuration', async (_name, payload) => {
    // Every one of these is truthy-or-absent nonsense from something other than this
    // server. Cached, each reads as "OIDC disabled" and leaves no way to sign in.
    mockHttpClient.mockResolvedValue(respondWith(payload));

    await expect(fetchServerConfig()).rejects.toThrow('Failed to load configuration');
  });

  it('accepts a configuration that carries oidc', async () => {
    mockHttpClient.mockResolvedValue(respondWith({ oidc: { enabled: false }, state_type: 'postgres' }));

    await expect(fetchServerConfig()).resolves.toEqual({
      oidc: { enabled: false },
      state_type: 'postgres',
    });
  });
});

describe('useServerConfig', () => {
  beforeEach(() => {
    mockHttpClient.mockReset();
    resetServerConfigCache();
  });

  it('reports neither a config nor an error while the request is in flight', () => {
    mockHttpClient.mockReturnValue(new Promise(() => {}));

    const { result } = renderHook(() => useServerConfig());

    // Both null is the "unknown" state callers default-deny on. Seeding config to {}
    // would make useOidcEnabled answer false during the request and fall open.
    expect(result.current).toEqual({ config: null, error: null });
  });

  it('reports the configuration once it arrives', async () => {
    mockHttpClient.mockResolvedValue(respondWith({ oidc: { enabled: true } }));

    const { result } = renderHook(() => useServerConfig());

    await waitFor(() => expect(result.current.config).toEqual({ oidc: { enabled: true } }));
    expect(result.current.error).toBeNull();
  });

  it('reports the failure and keeps the configuration unknown', async () => {
    mockHttpClient.mockRejectedValue(new Error('network'));

    const { result } = renderHook(() => useServerConfig());

    await waitFor(() => expect(result.current.error?.message).toBe('network'));
    expect(result.current.config).toBeNull();
  });
});
