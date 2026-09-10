import { afterEach, describe, expect, it, vi } from 'vitest';
import { blockStorageAccess } from '../../test/blockStorage';
import { installStorageShim } from './storageShim';

describe('installStorageShim', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
  });

  it('leaves a working localStorage alone', () => {
    localStorage.setItem('shim.probe', 'real');

    expect(installStorageShim()).toBe(false);
    expect(localStorage.getItem('shim.probe')).toBe('real');
  });

  it('replaces a throwing localStorage with a usable one', () => {
    const restore = blockStorageAccess();
    try {
      expect(() => window.localStorage).toThrow();
      expect(installStorageShim()).toBe(true);

      // Every later consumer, vendor code included, now reads a plain object.
      expect(() => window.localStorage).not.toThrow();

      const storage = window.localStorage;
      storage.setItem('shim.a', 'one');
      storage.setItem('shim.b', 'two');

      expect(storage.getItem('shim.a')).toBe('one');
      expect(storage.getItem('shim.absent')).toBeNull();
      expect(storage.length).toBe(2);
      expect(storage.key(0)).toBe('shim.a');
      expect(storage.key(1)).toBe('shim.b');
      expect(storage.key(5)).toBeNull();

      // Storage coerces values to strings; oidc-client-ts relies on it.
      storage.setItem('shim.n', 5 as unknown as string);
      expect(storage.getItem('shim.n')).toBe('5');

      storage.removeItem('shim.a');
      expect(storage.getItem('shim.a')).toBeNull();

      storage.clear();
      expect(storage.length).toBe(0);
      expect(storage.getItem('shim.b')).toBeNull();
    } finally {
      restore();
    }
  });

  // Some sandboxed iframes and old WebViews report the property as undefined
  // instead of throwing, and they need the shim just as much.
  it('replaces a localStorage that reads as undefined', () => {
    vi.stubGlobal('window', { localStorage: undefined });

    expect(installStorageShim()).toBe(true);
    expect(globalThis.window.localStorage.getItem('shim.absent')).toBeNull();
    globalThis.window.localStorage.setItem('shim.probe', 'value');
    expect(globalThis.window.localStorage.getItem('shim.probe')).toBe('value');
  });

  it('does nothing without a window', () => {
    vi.stubGlobal('window', undefined);

    expect(installStorageShim()).toBe(false);
  });

  // Stubbed rather than defined on the real global: a non-configurable property
  // cannot be deleted, and vitest's jsdom teardown deletes every global it set.
  it('gives up quietly when the property cannot be redefined', () => {
    const fakeWindow = {};
    Object.defineProperty(fakeWindow, 'localStorage', {
      configurable: false,
      get() {
        throw new DOMException('blocked', 'SecurityError');
      },
    });
    vi.stubGlobal('window', fakeWindow);

    expect(() => installStorageShim()).not.toThrow();
    expect(installStorageShim()).toBe(false);
  });
});
