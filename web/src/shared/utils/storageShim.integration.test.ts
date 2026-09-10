import mainSource from '../../main.tsx?raw';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { blockStorageAccess } from '../../test/blockStorage';
import { installStorageShim } from './storageShim';

describe('storage shim against react-admin', () => {
  let restore: (() => void) | undefined;

  afterEach(() => {
    restore?.();
    restore = undefined;
  });

  // react-admin's default store is built while its module evaluates, and
  // ra-core tests `window.localStorage == undefined` outside its own
  // try/catch — so without the shim this import throws and nothing renders.
  it('lets ra-core import and store when storage access throws', async () => {
    restore = blockStorageAccess();
    expect(installStorageShim()).toBe(true);

    const { localStorageStore } = await import('ra-core');
    const store = localStorageStore();
    store.setItem('shim.probe', 'value');

    expect(store.getItem('shim.probe')).toBe('value');

    // react-admin calls reset() on logout, and both it and listItems()
    // enumerate the Storage object rather than calling its methods.
    expect(store.listItems()).toEqual({ 'shim.probe': 'value' });

    store.reset();
    expect(store.getItem('shim.probe')).toBeUndefined();
    expect(store.listItems()).toEqual({});
  });

  // The bootstrap module is the middle link: main.tsx imports it for its side
  // effect alone, so nothing else proves that the import installs the shim.
  it('installs the shim just by being imported', async () => {
    restore = blockStorageAccess();
    vi.resetModules();

    await import('./storageShim.bootstrap');

    window.localStorage.setItem('shim.probe', 'value');
    expect(window.localStorage.getItem('shim.probe')).toBe('value');
  });

  // The shim only helps if it runs before react-admin is imported, and static
  // imports run in source order. Nothing observable at runtime can stand in
  // for this check: ra-core freezes localStorageAvailable at module scope.
  it('is the first import in the entrypoint', () => {
    const firstImport = mainSource
      .split('\n')
      .map(line => line.trim())
      .find(line => line.startsWith('import'));

    expect(firstImport).toMatch(/storageShim\.bootstrap/);
  });
});
