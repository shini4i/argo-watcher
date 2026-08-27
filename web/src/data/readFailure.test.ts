import { HttpError } from 'react-admin';
import { describe, expect, it } from 'vitest';
import { describeReadFailure } from './readFailure';

const providerUnavailable = () =>
  new HttpError('authentication provider unavailable', 503, {
    status: 'authentication provider unavailable',
    error: 'token validation failed',
  });

describe('describeReadFailure', () => {
  it('names the identity provider when the server could not validate the token', () => {
    const failure = describeReadFailure(providerUnavailable());

    expect(failure.title).toBe('Argo Watcher cannot verify your session');
    expect(failure.detail).toContain('identity provider');
    // The server never judged the token, so the copy must not claim it is valid.
    expect(failure.hint).toContain('has not been rejected');
    expect(failure.hint).not.toContain('still valid');
  });

  it('passes server text through untouched at the length limit', () => {
    const failure = describeReadFailure(new HttpError('x'.repeat(300), 500));

    expect(failure.detail).toBe(`${'x'.repeat(300)} (HTTP 500)`);
  });

  it('trims server text one character over the limit', () => {
    const failure = describeReadFailure(new HttpError('x'.repeat(301), 500));

    expect(failure.detail).toBe(`${'x'.repeat(300)}… (HTTP 500)`);
  });

  it('does not blame the identity provider for a 503 the server did not attribute to it', () => {
    const failure = describeReadFailure(
      new HttpError('down', 503, { status: 'down', error: 'state backend unreachable' }),
    );

    expect(failure.title).toBe('Argo Watcher is unavailable');
    expect(failure.detail).toBe('down');
    expect(failure.hint).not.toContain('identity provider');
  });

  it.each([401, 403])('reports a credential rejected with %i as a session problem', status => {
    const failure = describeReadFailure(new HttpError('You are not authorized', status));

    expect(failure.title).toBe('This session is no longer accepted');
    expect(failure.detail).toBe('You are not authorized');
    expect(failure.hint).toContain('sign in again');
  });

  it('reports a request that never reached the server as unreachable', () => {
    // httpClient reports both a network drop and its 30s abort with status 0.
    const timedOut = describeReadFailure(new HttpError('Request timed out', 0));
    expect(timedOut.title).toBe('Could not reach the Argo Watcher server');
    // The detail already says it timed out; the hint must not repeat it.
    expect(timedOut.hint).toBe('Check that the server is reachable, then retry.');
    expect(describeReadFailure(new Error('boom')).title).toBe('Could not reach the Argo Watcher server');
  });

  it('carries the status code for any other server error', () => {
    const failure = describeReadFailure(new HttpError('internal server error', 500));

    expect(failure.title).toBe('The Argo Watcher server returned an error');
    expect(failure.detail).toBe('internal server error (HTTP 500)');
  });

  // dataProvider.getOne throws with the real response status, so a body that reports an
  // error on a 2xx must not be dressed up as a missing task or an unreachable server.
  it('reports a success whose body carries an error as a server-reported error', () => {
    const failure = describeReadFailure(
      new HttpError('task not found', 200, { id: 'task-1', error: 'task not found' }),
    );

    expect(failure.title).toBe('The Argo Watcher server reported an error');
    expect(failure.detail).toBe('task not found');
    expect(failure.detail).not.toContain('HTTP');
  });

  // Only reached for a task that vanished under a loaded page; a first-load 404 is the
  // not-found card, which TaskShow renders instead of calling this.
  it('reports a 404 as a task that is gone, not a rejected request', () => {
    const failure = describeReadFailure(new HttpError('task not found', 404));

    expect(failure.title).toBe('This task is no longer available');
    expect(failure.detail).toBe('task not found');
  });

  it('carries the status code for a rejected request', () => {
    const failure = describeReadFailure(new HttpError('unsupported status filter', 400));

    expect(failure.title).toBe('The request was rejected');
    expect(failure.detail).toBe('unsupported status filter (HTTP 400)');
  });

  // A 503 from a proxy carries no JSON body at all, and a non-conformant one can put
  // anything in `status`. Neither may be read as an identity-provider outage.
  it.each([
    ['a body that is not an object', new HttpError('nope', 503, 'not an object')],
    ['a non-string status field', new HttpError('nope', 503, { status: 503 })],
    ['no body at all', new HttpError('Service Unavailable', 503)],
  ])('does not blame the provider for %s', (_label, error) => {
    const failure = describeReadFailure(error);

    expect(failure.title).toBe('Argo Watcher is unavailable');
    expect(failure.hint).not.toContain('identity provider');
  });
});
