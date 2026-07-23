import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { HttpError } from 'react-admin';
import { buildQueryString, httpClient, REQUEST_TIMEOUT_MS } from './httpClient';

const { getAccessTokenMock } = vi.hoisted(() => ({
  getAccessTokenMock: vi.fn(() => 'token-abc'),
}));

vi.mock('../auth/tokenStore', () => ({
  getAccessToken: getAccessTokenMock,
}));

const mockFetch = vi.fn();

describe('httpClient', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    vi.stubGlobal('fetch', mockFetch);
    getAccessTokenMock.mockReturnValue('token-abc');
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('performs request with bearer token and parses JSON', async () => {
    mockFetch.mockResolvedValue(
      new Response(JSON.stringify({ status: 'ok' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    const response = await httpClient('/status');
    expect(response.data).toEqual({ status: 'ok' });
    const [, init] = mockFetch.mock.calls[0];
    expect(init?.headers).toMatchObject({
      Authorization: 'Bearer token-abc',
      'Oidc-Authorization': 'Bearer token-abc',
    });
  });

  it('respects explicit Authorization headers and mirrors them to the OIDC header', async () => {
    mockFetch.mockResolvedValue(
      new Response(JSON.stringify({ status: 'ok' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await httpClient('/status', {
      headers: { Authorization: 'Custom foo' },
    });

    const [, init] = mockFetch.mock.calls[0];
    expect(init?.headers).toMatchObject({
      Authorization: 'Custom foo',
      'Oidc-Authorization': 'Custom foo',
    });
  });

  it('serializes JSON request bodies for non-GET methods', async () => {
    getAccessTokenMock.mockReturnValue(undefined);
    const payload = { hello: 'world' };
    mockFetch.mockResolvedValue(
      new Response(JSON.stringify({ status: 'ok' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await httpClient('/status', { method: 'POST', body: payload });

    const [, init] = mockFetch.mock.calls[0];
    expect(init?.body).toBe(JSON.stringify(payload));
    expect(init?.headers).toMatchObject({ 'Content-Type': 'application/json' });
  });

  it('throws HttpError when server returns error payload', async () => {
    mockFetch.mockResolvedValue(
      new Response(JSON.stringify({ error: 'nope' }), {
        status: 400,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await expect(httpClient('/status')).rejects.toBeInstanceOf(HttpError);
  });

  it('throws HttpError on network failure', async () => {
    mockFetch.mockRejectedValue(new Error('dns'));
    await expect(httpClient('/status')).rejects.toBeInstanceOf(HttpError);
  });

  it('passes an AbortSignal to fetch so a hung request cannot wait forever', async () => {
    mockFetch.mockResolvedValue(
      new Response(JSON.stringify({ status: 'ok' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await httpClient('/status');

    const [, init] = mockFetch.mock.calls[0];
    expect(init?.signal).toBeInstanceOf(AbortSignal);
  });

  it('aborts a hung request after REQUEST_TIMEOUT_MS and rejects as a timeout', async () => {
    vi.useFakeTimers();
    try {
      // fetch never settles on its own; it rejects only once its signal aborts,
      // so the rejection can come solely from httpClient's own timeout wiring.
      mockFetch.mockImplementation(
        (_url: string, init: RequestInit) =>
          new Promise((_resolve, reject) => {
            const signal = init?.signal as AbortSignal;
            signal?.addEventListener('abort', () =>
              reject(new DOMException('The operation was aborted.', 'AbortError')),
            );
          }),
      );

      const promise = httpClient('/status');
      // Attach the rejection assertion before advancing so there is no window
      // where the rejection is unhandled.
      const assertion = expect(promise).rejects.toThrow(/timed out/i);
      await vi.advanceTimersByTimeAsync(REQUEST_TIMEOUT_MS);
      await assertion;

      const init = mockFetch.mock.calls[0][1] as RequestInit;
      expect((init.signal as AbortSignal).aborted).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it('clears the timeout on the success path so no late abort fires', async () => {
    vi.useFakeTimers();
    try {
      mockFetch.mockResolvedValue(
        new Response(JSON.stringify({ status: 'ok' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const response = await httpClient<{ status: string }>('/status');
      expect(response.data).toEqual({ status: 'ok' });

      // Advancing past the timeout must not abort the already-completed request.
      const init = mockFetch.mock.calls[0][1] as RequestInit;
      await vi.advanceTimersByTimeAsync(REQUEST_TIMEOUT_MS * 2);
      expect((init.signal as AbortSignal).aborted).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  it('returns undefined when response is not JSON', async () => {
    mockFetch.mockResolvedValue(
      new Response('plain', {
        status: 200,
        headers: { 'Content-Type': 'text/plain' },
      }),
    );

    const response = await httpClient('/status');
    expect(response.data).toBeUndefined();
  });

  it('throws parsing errors for malformed JSON payloads', async () => {
    mockFetch.mockResolvedValue(
      new Response('{"invalid"', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await expect(httpClient('/status')).rejects.toThrow('Failed to parse server response');
  });

  it('does not throw on 2xx responses that carry an error field', async () => {
    mockFetch.mockResolvedValue(
      new Response(JSON.stringify({ status: 'degraded', error: 'argocd is unavailable' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    const response = await httpClient<{ status: string; error: string }>('/status');
    expect(response.status).toBe(200);
    expect(response.data).toEqual({ status: 'degraded', error: 'argocd is unavailable' });
  });

  it('prefers descriptive fields when building HttpErrors', async () => {
    mockFetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ status: 'custom-status' }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await expect(httpClient('/status')).rejects.toThrow('custom-status');

    mockFetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'custom-error' }), {
        status: 500,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await expect(httpClient('/status')).rejects.toThrow('custom-error');
  });
});

describe('buildQueryString', () => {
  it('skips empty parameters and encodes values', () => {
    expect(buildQueryString({ a: 1, b: '', c: 'value with space' })).toBe('?a=1&c=value+with+space');
  });
});
