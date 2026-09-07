import { useCallback, useEffect, useMemo } from 'react';
import { useStore } from 'react-admin';
import { useSearchParams } from 'react-router-dom';

export const PINNED_APPS_STORE_KEY = 'argo-watcher.pinnedApps';

/** Keeps a shared `?pinned=` link, and the stored list, to a sane length. */
export const MAX_PINNED_APPS = 20;

/** Drops blanks and duplicates, preserving first-seen order, and caps the list. */
export const normalizePinned = (values: readonly string[]): string[] => {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const value of values) {
    const trimmed = value.trim();
    if (!trimmed || seen.has(trimmed)) {
      continue;
    }
    seen.add(trimmed);
    result.push(trimmed);
    if (result.length >= MAX_PINNED_APPS) {
      break;
    }
  }
  return result;
};

export const parsePinnedParam = (raw: string | null): string[] =>
  raw === null ? [] : normalizePinned(raw.split(','));

interface PinnedAppsState {
  readonly pinned: string[];
  readonly pin: (app: string) => void;
  readonly unpin: (app: string) => void;
  readonly isPinned: (app: string) => boolean;
  /** Absolute URL that reproduces this pin set for someone else. */
  readonly shareLink: string;
}

/**
 * @description Per-browser pinned applications, held in react-admin's Store
 * (localStorage) so anonymous mode works too. A `?pinned=` parameter wins on
 * load and is adopted into the store, which is how a view gets shared; there is
 * no cross-device sync without a user-preferences endpoint.
 */
export const usePinnedApps = (): PinnedAppsState => {
  const [stored, setStored] = useStore<string[]>(PINNED_APPS_STORE_KEY, []);
  const [searchParams, setSearchParams] = useSearchParams();
  const fromUrl = searchParams.get('pinned');

  useEffect(() => {
    if (fromUrl === null) {
      return;
    }
    const adopted = parsePinnedParam(fromUrl);
    setStored(adopted);
    const next = new URLSearchParams(searchParams);
    next.delete('pinned');
    setSearchParams(next, { replace: true });
    // Runs only while the param is present; adopting it once is the whole job.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fromUrl]);

  const pinned = useMemo(() => normalizePinned(stored ?? []), [stored]);

  const pin = useCallback(
    (app: string) => setStored(current => normalizePinned([...(current ?? []), app])),
    [setStored],
  );

  const unpin = useCallback(
    (app: string) => setStored(current => (current ?? []).filter(entry => entry !== app)),
    [setStored],
  );

  const isPinned = useCallback((app: string) => pinned.includes(app), [pinned]);

  const shareLink = useMemo(() => {
    const base = globalThis.location?.href ?? '';
    if (!base) {
      return '';
    }
    try {
      const url = new URL(base);
      if (pinned.length === 0) {
        url.searchParams.delete('pinned');
      } else {
        url.searchParams.set('pinned', pinned.join(','));
      }
      return url.toString();
    } catch {
      return base;
    }
  }, [pinned]);

  return { pinned, pin, unpin, isPinned, shareLink };
};
