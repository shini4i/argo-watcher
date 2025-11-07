import { describe, expect, it } from 'vitest';
import { hasPrivilegedAccess } from './permissions';

describe('permissions utilities', () => {
  it('returns true when user belongs to privileged group', () => {
    expect(hasPrivilegedAccess(['users', 'admins'], ['admins'])).toBe(true);
  });

  it('returns false when lists do not intersect', () => {
    expect(hasPrivilegedAccess(['users'], ['deploy'])).toBe(false);
  });

  it('returns false when inputs are invalid', () => {
    expect(hasPrivilegedAccess(null, ['admins'])).toBe(false);
  });
});
