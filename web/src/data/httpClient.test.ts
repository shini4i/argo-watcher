import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { HttpError } from 'react-admin';
import { httpClient, buildQueryString } from './httpClient';

vi.mock('../auth/tokenStore', () => ({
  getAccessToken: vi.fn(() => 'token-abc'),
}));

const mockFetch = vi.fn();

describe('httpClient', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    vi.stubGlobal('fetch', mockFetch);
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
      'Keycloak-Authorization': 'Bearer token-abc',
    });
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
});

describe('buildQueryString', () => {
  it('skips empty parameters and encodes values', () => {
    expect(buildQueryString({ a: 1, b: '', c: 'value with space' })).toBe('?a=1&c=value+with+space');
  });
});
