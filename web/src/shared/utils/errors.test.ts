import { describe, expect, it } from 'vitest';
import { HttpError } from 'react-admin';
import { normalizeError } from './errors';

describe('errors utilities', () => {
  it('normalizes HttpError instances', () => {
    const error = new HttpError('Forbidden', 403, { reason: 'not allowed' });
    const result = normalizeError(error);
    expect(result.message).toBe('Forbidden');
    expect(result.status).toBe(403);
    expect(result.details).toEqual({ reason: 'not allowed' });
  });

  it('normalizes generic errors', () => {
    const error = new Error('boom');
    const result = normalizeError(error);
    expect(result.message).toBe('boom');
  });

  it('returns fallback for unknown types', () => {
    const result = normalizeError(42);
    expect(result.message).toBe('An unexpected error occurred');
    expect(result.details).toBe(42);
  });
});
