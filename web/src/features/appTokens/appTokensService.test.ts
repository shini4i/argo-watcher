import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  type AppToken,
  describeScope,
  isTokenActive,
  issueAppToken,
  listAppTokens,
  revokeAppToken,
} from './appTokensService';

const jsonResponse = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });

const token = (overrides: Partial<AppToken> = {}): AppToken => ({
  id: 'a1',
  all_apps: false,
  apps: ['app1'],
  hint: '3f9a',
  created_by: 'alice',
  created_at: 1_700_000_000_000,
  ...overrides,
});

describe('appTokensService', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  describe('listAppTokens', () => {
    it('returns the tokens the server reports', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse([token()]));

      await expect(listAppTokens()).resolves.toEqual([token()]);
    });

    it('refuses a bodiless 200 rather than reporting no tokens', async () => {
      // writeJSON always marshals a body, so an absent one means something other
      // than the API answered.
      vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 200 }));

      await expect(listAppTokens()).rejects.toThrow(/did not answer/);
    });

    it('propagates a rejection so the page can surface it', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValue(
        jsonResponse({ status: 'unauthorized' }, 401),
      );

      await expect(listAppTokens()).rejects.toThrow(/unauthorized/);
    });
  });

  describe('issueAppToken', () => {
    it('posts the requested scope and returns the secret', async () => {
      const fetchSpy = vi
        .spyOn(globalThis, 'fetch')
        .mockResolvedValue(jsonResponse(token({ secret: 'awt_secret' }), 201));

      const issued = await issueAppToken({ apps: ['app1', 'app2'], expires_in_days: 30 });

      expect(issued.secret).toBe('awt_secret');

      const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
      expect(init.method).toBe('POST');
      expect(JSON.parse(init.body as string)).toEqual({
        apps: ['app1', 'app2'],
        expires_in_days: 30,
      });
    });

    it('fails loudly when the server returns no payload', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 201 }));

      await expect(issueAppToken({ all_apps: true })).rejects.toThrow(/no payload/);
    });
  });

  describe('revokeAppToken', () => {
    it('deletes by id', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse('revoked'));

      await revokeAppToken('a1');

      const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
      expect(url).toContain('/api/v1/app-tokens/a1');
      expect(init.method).toBe('DELETE');
    });

    it('encodes an id that would otherwise change the path', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse('revoked'));

      await revokeAppToken('a/../b');

      const [url] = fetchSpy.mock.calls[0] as [string];
      expect(url).toContain('a%2F..%2Fb');
    });
  });

  describe('isTokenActive', () => {
    const now = 1_700_000_000_000;

    it.each([
      ['a token without an expiry', token(), true],
      ['a token expiring later', token({ expires_at: now + 1000 }), true],
      ['a token expiring now', token({ expires_at: now }), false],
      ['an expired token', token({ expires_at: now - 1000 }), false],
      ['a revoked token', token({ revoked_at: now - 1000 }), false],
      ['a revoked token with a future expiry', token({ revoked_at: now, expires_at: now + 1000 }), false],
    ])('%s', (_name, subject, expected) => {
      expect(isTokenActive(subject as AppToken, now)).toBe(expected);
    });
  });

  describe('describeScope', () => {
    it('names the wildcard', () => {
      expect(describeScope(token({ all_apps: true, apps: undefined }))).toBe('All applications');
    });

    it('lists the applications', () => {
      expect(describeScope(token({ apps: ['app1', 'app2'] }))).toBe('app1, app2');
    });

    it('does not pretend an empty scope covers something', () => {
      expect(describeScope(token({ apps: [] }))).toBe('No applications');
    });
  });
});

describe('the SPA catch-all must never read as success', () => {
  // Where the token endpoints are not registered the request falls through to the
  // Web UI's HTML handler, which answers 200. Reporting that as a completed
  // revocation would tell an operator a leaked credential is dead while it deploys.
  const htmlResponse = () =>
    new Response('<!doctype html><title>Argo Watcher</title>', {
      status: 200,
      headers: { 'Content-Type': 'text/html; charset=utf-8' },
    });

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('fails a revocation the API did not confirm', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(htmlResponse());

    await expect(revokeAppToken('a1')).rejects.toThrow(/did not answer the deploy token revoke/);
  });

  it('fails a listing the API did not answer, rather than reporting no tokens', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(htmlResponse());

    await expect(listAppTokens()).rejects.toThrow(/did not answer the deploy token list/);
  });

  it('still accepts a genuine JSON answer', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify('application deploy token revoked'), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await expect(revokeAppToken('a1')).resolves.toBeUndefined();
  });

  it('still accepts an empty list from the API', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify([]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );

    await expect(listAppTokens()).resolves.toEqual([]);
  });
});
