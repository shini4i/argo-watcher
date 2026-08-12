import { describe, expect, it } from 'vitest';
import {
  describeCallbackError,
  describeIncompleteConfig,
  describeRedirectError,
} from './authFailure';

// Mirrors what oidc-client-ts throws for a provider error response: an
// ErrorResponse carrying the `error`, `error_description` and `error_uri` params.
const errorResponse = (overrides: Record<string, unknown> = {}) =>
  Object.assign(new Error('access_denied'), {
    name: 'ErrorResponse',
    error: 'access_denied',
    error_description: 'User is not assigned to the client application.',
    error_uri: null,
    ...overrides,
  });

describe('describeCallbackError', () => {
  it('surfaces the provider error code and description verbatim', () => {
    const failure = describeCallbackError(errorResponse());

    expect(failure.kind).toBe('provider_error');
    expect(failure.code).toBe('access_denied');
    expect(failure.detail).toBe('User is not assigned to the client application.');
  });

  it('keeps the provider documentation link when one is supplied', () => {
    const failure = describeCallbackError(errorResponse({ error_uri: 'https://idp/docs/err' }));

    expect(failure.uri).toBe('https://idp/docs/err');
  });

  // Only a real, followable address is worth offering. Everything else — a
  // relative path, a bare phrase, or a script payload arriving through the
  // callback query — is dropped rather than rendered as a link.
  it.each([
    { label: 'a relative path', uri: '/docs/errors' },
    { label: 'plain text', uri: 'see the manual' },
    { label: 'a javascript: payload', uri: 'javascript:alert(1)' },
    { label: 'a data: payload', uri: 'data:text/html,x' },
  ])('drops $label instead of linking it', ({ uri }) => {
    expect(describeCallbackError(errorResponse({ error_uri: uri })).uri).toBeUndefined();
  });

  it('drops a link too long to be shown in full rather than linking a truncated URL', () => {
    // Truncating a URL cannot move its host, so the link would still resolve —
    // to a path the provider does not have. A 404 is worse than no link.
    const uri = `https://idp.example.com/docs/${'p'.repeat(400)}`;

    expect(describeCallbackError(errorResponse({ error_uri: uri })).uri).toBeUndefined();
  });

  it('keeps an http(s) link', () => {
    expect(describeCallbackError(errorResponse({ error_uri: 'https://idp/docs' })).uri).toBe(
      'https://idp/docs',
    );
    expect(describeCallbackError(errorResponse({ error_uri: 'http://idp/docs' })).uri).toBe(
      'http://idp/docs',
    );
  });

  it('omits an absent description and link instead of rendering null', () => {
    const failure = describeCallbackError(
      errorResponse({ error_description: null, error_uri: null }),
    );

    expect(failure.detail).toBeUndefined();
    expect(failure.uri).toBeUndefined();
  });

  it('truncates provider-supplied text so one long description cannot flood the screen', () => {
    const failure = describeCallbackError(errorResponse({ error_description: 'x'.repeat(900) }));

    expect(failure.detail!.length).toBeLessThanOrEqual(301);
    expect(failure.detail!.endsWith('…')).toBe(true);
  });

  it('truncates at the display budget, not before it', () => {
    const exact = describeCallbackError(errorResponse({ error_description: 'x'.repeat(300) }));
    const over = describeCallbackError(errorResponse({ error_description: 'x'.repeat(301) }));

    expect(exact.detail).toBe('x'.repeat(300));
    expect(over.detail).toBe(`${'x'.repeat(300)}…`);
  });

  it('keeps a link at the budget and drops the one past it', () => {
    const pad = (length: number) => `https://idp/d/${'p'.repeat(length - 'https://idp/d/'.length)}`;

    expect(describeCallbackError(errorResponse({ error_uri: pad(300) })).uri).toBe(pad(300));
    expect(describeCallbackError(errorResponse({ error_uri: pad(301) })).uri).toBeUndefined();
  });

  // On the token-exchange path oidc-client-ts builds ErrorResponse straight from
  // the provider's parsed JSON body (no coercion), so these fields are only
  // strings if the provider is conformant. Throwing here would reject
  // bootstrapAuth, mount the app on a failed callback, and restore the redirect
  // loop — so the mapper has to survive any JSON value.
  it.each([
    { label: 'a number description', overrides: { error_description: 42 } },
    { label: 'an object description', overrides: { error_description: { msg: 'nope' } } },
    { label: 'an array description', overrides: { error_description: ['a', 'b'] } },
    { label: 'a number uri', overrides: { error_uri: 7 } },
    { label: 'an object uri', overrides: { error_uri: {} } },
  ])('survives $label from a non-conformant provider', ({ overrides }) => {
    const failure = describeCallbackError(errorResponse(overrides));

    expect(failure.kind).toBe('provider_error');
    expect(failure.code).toBe('access_denied');
    // Unusable values are dropped, never coerced onto the screen.
    if ('error_description' in overrides) {
      expect(failure.detail).toBeUndefined();
    }
    expect(failure.uri).toBeUndefined();
  });

  it('drops whitespace-only provider text', () => {
    const failure = describeCallbackError(
      errorResponse({ error_description: '   ', error_uri: '  ' }),
    );

    expect(failure.detail).toBeUndefined();
    expect(failure.uri).toBeUndefined();
  });

  it('does not treat a whitespace-only error code as a provider rejection', () => {
    // An empty code carries no diagnosis, so presenting it as the provider's
    // verdict would be a title with nothing behind it.
    const failure = describeCallbackError(errorResponse({ error: '   ' }));

    expect(failure.kind).toBe('callback_failed');
    expect(failure.code).toBeUndefined();
  });

  it('points at the client registration for a code it does not special-case', () => {
    // server_error, login_required and friends are common from a broken provider;
    // the code is echoed and the hint must still say where to look.
    const failure = describeCallbackError(errorResponse({ error: 'server_error' }));

    expect(failure.code).toBe('server_error');
    expect(failure.hint).toMatch(/client registration/i);
  });

  it.each([
    { label: 'undefined', thrown: undefined },
    { label: 'a plain object', thrown: { some: 'object' } },
  ])('produces a usable failure for $label', ({ thrown }) => {
    const failure = describeCallbackError(thrown);

    expect(failure.kind).toBe('callback_failed');
    expect(failure.title).not.toBe('');
    // Better an empty detail than "[object Object]" on screen.
    expect(failure.detail).toBeUndefined();
  });

  it('names the rejected scope for invalid_scope, the most common client mistake', () => {
    const failure = describeCallbackError(errorResponse({ error: 'invalid_scope' }));

    // The hint has to name the exact scopes, or the operator cannot tell which
    // one the provider refused.
    expect(failure.hint).toContain('openid profile email');
  });

  it('matches the hint on a padded code, as shown', () => {
    // The code is displayed trimmed, so the hint must be chosen from the same
    // value or a proxy's stray whitespace silently costs the specific guidance.
    const failure = describeCallbackError(errorResponse({ error: ' invalid_scope ' }));

    expect(failure.code).toBe('invalid_scope');
    expect(failure.hint).toContain('openid profile email');
  });

  it('points at the client id for invalid_client', () => {
    const failure = describeCallbackError(errorResponse({ error: 'invalid_client' }));

    expect(failure.hint).toContain('OIDC_CLIENT_ID');
  });

  it('points at the client id for unauthorized_client too', () => {
    const failure = describeCallbackError(errorResponse({ error: 'unauthorized_client' }));

    expect(failure.hint).toContain('OIDC_CLIENT_ID');
  });

  it('reports a local exchange failure separately from a provider rejection', () => {
    // Thrown by oidc-client-ts when the stored PKCE state is gone — a different
    // problem from the provider refusing the request, so it must not be
    // presented as a provider error.
    const failure = describeCallbackError(new Error('No matching state found in storage'));

    expect(failure.kind).toBe('callback_failed');
    expect(failure.code).toBeUndefined();
    expect(failure.detail).toBe('No matching state found in storage');
  });

  it('still produces a usable failure for a non-Error throw', () => {
    const failure = describeCallbackError('boom');

    expect(failure.kind).toBe('callback_failed');
    expect(failure.title).not.toBe('');
    expect(failure.detail).toBe('boom');
  });
});

describe('describeRedirectError', () => {
  it('claims only that the sign-in did not start, whatever the cause', () => {
    // signinRedirect also rejects when the PKCE state cannot be stored, so a
    // headline asserting the provider is unreachable would be false there and
    // would send the operator after the wrong problem.
    const failure = describeRedirectError(new Error('storage is full'), 'https://idp/realms/demo');

    expect(failure.title).toBe('Could not start the sign-in');
    expect(failure.detail).toBe('storage is full');
  });

  it('names the issuer discovery URL the browser has to reach', () => {
    const failure = describeRedirectError(
      new Error('Failed to fetch'),
      'https://idp.example.com/realms/demo',
    );

    expect(failure.kind).toBe('redirect_failed');
    expect(failure.detail).toBe('Failed to fetch');
    // The whole point of this screen: an issuer reachable from the server but not
    // from the browser is the failure operators cannot diagnose otherwise.
    expect(failure.hint).toContain(
      'https://idp.example.com/realms/demo/.well-known/openid-configuration',
    );
    expect(failure.hint).toContain('browser');
  });

  it.each([
    { label: 'one trailing slash', issuer: 'https://idp/app/o/aw/' },
    { label: 'several trailing slashes', issuer: 'https://idp/app/o/aw///' },
    { label: 'no trailing slash', issuer: 'https://idp/app/o/aw' },
  ])('builds the discovery URL with exactly one slash given $label', ({ issuer }) => {
    const failure = describeRedirectError(new Error('Failed to fetch'), issuer);

    expect(failure.hint).toContain('https://idp/app/o/aw/.well-known/openid-configuration');
  });

  it('falls back to generic wording when the issuer is unknown', () => {
    const failure = describeRedirectError(new Error('Failed to fetch'), undefined);

    expect(failure.kind).toBe('redirect_failed');
    expect(failure.hint).not.toContain('undefined');
  });

  it('falls back to generic wording for an issuer that is not a string', () => {
    // The issuer comes from /api/v1/config JSON, which is parsed but not validated.
    const failure = describeRedirectError(new Error('Failed to fetch'), 42 as unknown as string);

    expect(failure.kind).toBe('redirect_failed');
    expect(failure.hint).not.toContain('42');
  });
});

describe('describeIncompleteConfig', () => {
  it('lists the missing fields and the variables that set them', () => {
    const failure = describeIncompleteConfig(['issuer_url', 'client_id']);

    expect(failure.kind).toBe('config_incomplete');
    expect(failure.detail).toContain('issuer_url');
    expect(failure.detail).toContain('client_id');
    expect(failure.hint).toContain('OIDC_ISSUER_URL');
    expect(failure.hint).toContain('OIDC_CLIENT_ID');
  });
});
