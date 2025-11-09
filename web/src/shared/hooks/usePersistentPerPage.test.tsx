import { describe, expect, it, beforeEach, vi } from 'vitest';
import { __testing, readPersistentPerPage } from './usePersistentPerPage';

describe('usePersistentPerPage', () => {
  beforeEach(() => {
    globalThis.window?.localStorage?.clear();
  });

  it('reads stored per-page value and falls back when invalid or missing', () => {
    globalThis.window?.localStorage?.setItem('recent.perPage', '50');
    expect(readPersistentPerPage('recent.perPage', 25)).toBe(50);

    globalThis.window?.localStorage?.setItem('recent.perPage', 'invalid');
    expect(readPersistentPerPage('recent.perPage', 25)).toBe(25);
  });

  it('falls back when browser window is unavailable', () => {
    const originalWindow = globalThis.window;
    // eslint-disable-next-line @typescript-eslint/no-dynamic-delete, @typescript-eslint/ban-ts-comment
    // @ts-ignore
    delete globalThis.window;
    expect(readPersistentPerPage('history.perPage', 10)).toBe(10);
    globalThis.window = originalWindow;
  });

  it('writes per-page values via helper utilities', () => {
    const setItemSpy = vi.spyOn(Storage.prototype, 'setItem');
    __testing.writePerPage('tasks.perPage', 75);
    expect(setItemSpy).toHaveBeenCalledWith('tasks.perPage', '75');
  });

  it('skips writes when window is unavailable', () => {
    const originalWindow = globalThis.window;
    // eslint-disable-next-line @typescript-eslint/no-dynamic-delete, @typescript-eslint/ban-ts-comment
    // @ts-ignore
    delete globalThis.window;
    expect(() => __testing.writePerPage('tasks.perPage', 20)).not.toThrow();
    globalThis.window = originalWindow;
  });
});
