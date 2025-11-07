import { useEffect } from 'react';
import { useListPaginationContext } from 'react-admin';

/** Retrieves the stored per-page value or falls back when absent/invalid. */
const readPerPage = (storageKey: string, fallback: number) => {
  if (typeof window === 'undefined') {
    return fallback;
  }

  const raw = window.localStorage.getItem(storageKey);
  const parsed = raw ? Number.parseInt(raw, 10) : Number.NaN;

  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
};

/** Persists the current per-page value to localStorage. */
const writePerPage = (storageKey: string, value: number) => {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.setItem(storageKey, String(value));
};

/**
 * Reads the persisted `perPage` preference, falling back to the provided default when none is stored.
 */
export const readPersistentPerPage = (storageKey: string, fallback: number) => readPerPage(storageKey, fallback);

/**
 * React component that synchronizes the current list `perPage` value with local storage.
 * Must be rendered within a React-admin `<List>` so that the pagination context is available.
 */
export const PerPagePersistence = ({ storageKey }: { storageKey: string }) => {
  const { perPage } = useListPaginationContext();

  useEffect(() => {
    if (perPage) {
      writePerPage(storageKey, perPage);
    }
  }, [perPage, storageKey]);

  return null;
};
