import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { localStorageStore, StoreContextProvider } from 'react-admin';
import { beforeEach, describe, expect, it } from 'vitest';
import {
  MAX_PINNED_APPS,
  normalizePinned,
  parsePinnedParam,
  PINNED_APPS_STORE_KEY,
  usePinnedApps,
} from './usePinnedApps';

// The real localStorage store, since persisting across reloads is the point of
// the feature. <Admin> supplies this provider in the app.
const wrapper = (initialEntry: string) =>
  ({ children }: { children: ReactNode }) => (
    <StoreContextProvider value={localStorageStore()}>
      <MemoryRouter initialEntries={[initialEntry]}>{children}</MemoryRouter>
    </StoreContextProvider>
  );

describe('normalizePinned', () => {
  it('drops blanks and trims', () => {
    expect(normalizePinned([' a ', '', '   ', 'b'])).toEqual(['a', 'b']);
  });

  it('removes duplicates, keeping first-seen order', () => {
    expect(normalizePinned(['b', 'a', 'b'])).toEqual(['b', 'a']);
  });

  it('caps the list so a shared link cannot grow without bound', () => {
    const many = Array.from({ length: MAX_PINNED_APPS + 5 }, (_unused, i) => `app-${i}`);
    expect(normalizePinned(many)).toHaveLength(MAX_PINNED_APPS);
  });
});

describe('parsePinnedParam', () => {
  it('splits a comma list', () => {
    expect(parsePinnedParam('a,b,c')).toEqual(['a', 'b', 'c']);
  });

  it('treats an absent param as no pins', () => {
    expect(parsePinnedParam(null)).toEqual([]);
  });

  it('treats an empty param as no pins', () => {
    expect(parsePinnedParam('')).toEqual([]);
  });
});

describe('usePinnedApps', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('starts with nothing pinned', () => {
    const { result } = renderHook(() => usePinnedApps(), { wrapper: wrapper('/overview') });
    expect(result.current.pinned).toEqual([]);
  });

  it('pins and unpins an app', () => {
    const { result } = renderHook(() => usePinnedApps(), { wrapper: wrapper('/overview') });

    act(() => result.current.pin('checkout'));
    expect(result.current.pinned).toEqual(['checkout']);
    expect(result.current.isPinned('checkout')).toBe(true);

    act(() => result.current.unpin('checkout'));
    expect(result.current.pinned).toEqual([]);
    expect(result.current.isPinned('checkout')).toBe(false);
  });

  // The store namespaces its keys, so persistence is asserted through a fresh
  // mount rather than by reading a raw localStorage key.
  it('keeps the pins across a remount', () => {
    const first = renderHook(() => usePinnedApps(), { wrapper: wrapper('/overview') });
    act(() => first.result.current.pin('checkout'));
    first.unmount();

    const second = renderHook(() => usePinnedApps(), { wrapper: wrapper('/overview') });
    expect(second.result.current.pinned).toEqual(['checkout']);
    expect(JSON.stringify(localStorage)).toContain(PINNED_APPS_STORE_KEY);
  });

  it('never pins the same app twice', () => {
    const { result } = renderHook(() => usePinnedApps(), { wrapper: wrapper('/overview') });

    act(() => result.current.pin('checkout'));
    act(() => result.current.pin('checkout'));

    expect(result.current.pinned).toEqual(['checkout']);
  });

  // The link is the sharing story, so it must win over whatever this browser
  // had pinned before.
  it('adopts a ?pinned= link over the stored list', () => {
    localStorage.setItem(PINNED_APPS_STORE_KEY, JSON.stringify(['stale']));

    const { result } = renderHook(() => usePinnedApps(), {
      wrapper: wrapper('/overview?pinned=alpha,beta'),
    });

    expect(result.current.pinned).toEqual(['alpha', 'beta']);
  });

  it('de-duplicates and caps a hostile ?pinned= link', () => {
    const many = Array.from({ length: MAX_PINNED_APPS + 10 }, (_unused, i) => `app-${i}`).join(',');

    const { result } = renderHook(() => usePinnedApps(), {
      wrapper: wrapper(`/overview?pinned=${many},app-0,app-0`),
    });

    expect(result.current.pinned).toHaveLength(MAX_PINNED_APPS);
  });

  it('offers a share link carrying the current pins', () => {
    const { result } = renderHook(() => usePinnedApps(), { wrapper: wrapper('/overview') });

    act(() => result.current.pin('checkout'));
    act(() => result.current.pin('payments'));

    expect(result.current.shareLink).toContain('pinned=checkout%2Cpayments');
  });

  it('leaves the pinned param out of the link when nothing is pinned', () => {
    const { result } = renderHook(() => usePinnedApps(), { wrapper: wrapper('/overview') });
    expect(result.current.shareLink).not.toContain('pinned=');
  });
});
