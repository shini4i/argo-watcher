import { afterEach, describe, expect, it, vi } from 'vitest';
import { blockStorageAccess } from '../../test/blockStorage';
import { safeGetItem, safeRemoveItem, safeSetItem } from './storage';

describe('safe storage helpers', () => {
  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('round-trips a value', () => {
    safeSetItem('probe.key', 'value');
    expect(safeGetItem('probe.key')).toBe('value');

    safeRemoveItem('probe.key');
    expect(safeGetItem('probe.key')).toBeNull();
  });

  it('reports a missing key as null', () => {
    expect(safeGetItem('probe.absent')).toBeNull();
  });

  it('survives storage whose property access throws', () => {
    const restore = blockStorageAccess();
    try {
      expect(safeGetItem('probe.key')).toBeNull();
      expect(() => safeSetItem('probe.key', 'value')).not.toThrow();
      expect(() => safeRemoveItem('probe.key')).not.toThrow();
    } finally {
      restore();
    }
  });

  it('survives a getItem that throws', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });

    expect(safeGetItem('probe.key')).toBeNull();
  });

  it('swallows a full quota on write', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('quota', 'QuotaExceededError');
    });

    expect(() => safeSetItem('probe.key', 'value')).not.toThrow();
  });

  it('swallows a removeItem that throws', () => {
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });

    expect(() => safeRemoveItem('probe.key')).not.toThrow();
  });
});
