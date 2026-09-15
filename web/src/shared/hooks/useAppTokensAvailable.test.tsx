import { renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { resetServerConfigCache } from '../../data/serverConfig';
import { useAppTokensAvailable } from './useAppTokensAvailable';

const jsonResponse = (body: unknown) =>
  new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });

describe('useAppTokensAvailable', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    // The configuration is cached for the page's lifetime, so without this every case
    // after the first would assert against the first one's response.
    resetServerConfigCache();
  });

  it('starts unknown so callers deny before the config arrives', () => {
    vi.spyOn(globalThis, 'fetch').mockReturnValue(new Promise(() => {}));

    const { result } = renderHook(() => useAppTokensAvailable());

    expect(result.current).toBeNull();
  });

  it.each([
    ['OIDC and Postgres', { oidc: { enabled: true }, state_type: 'postgres' }, true],
    ['Postgres but no OIDC', { oidc: { enabled: false }, state_type: 'postgres' }, false],
    ['OIDC but in-memory state', { oidc: { enabled: true }, state_type: 'in-memory' }, false],
    ['neither', { oidc: { enabled: false }, state_type: 'in-memory' }, false],
    ['a payload missing both fields', {}, false],
  ])('reports %s', async (_name, config, expected) => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(config));

    const { result } = renderHook(() => useAppTokensAvailable());

    await waitFor(() => {
      expect(result.current).toBe(expected);
    });
  });

  it('stays unknown when the config answers 200 with no JSON body', async () => {
    // Distinct from the `{}` payload case above: an empty body is a proxy or the SPA
    // catch-all answering, not a server that reported its configuration.
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('<html></html>', { status: 200, headers: { 'Content-Type': 'text/html' } }),
    );

    const { result } = renderHook(() => useAppTokensAvailable());

    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    expect(result.current).toBeNull();
  });

  it('stays unknown when the config request fails', async () => {
    // Collapsing to false would hide the feature on a blip; to true would offer a
    // page that cannot work.
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network error'));

    const { result } = renderHook(() => useAppTokensAvailable());

    // Settle on the request having been made and rejected, not on a fixed delay:
    // a sleep would pass for the wrong reason on a loaded runner.
    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    expect(result.current).toBeNull();
  });
});
