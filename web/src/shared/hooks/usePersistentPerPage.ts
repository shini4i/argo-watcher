import { useEffect } from 'react';
import { useListPaginationContext } from 'react-admin';
import { safeGetItem, safeSetItem } from '../utils';

const readPerPage = (storageKey: string, fallback: number) => {
  const raw = safeGetItem(storageKey);
  const parsed = raw ? Number.parseInt(raw, 10) : Number.NaN;

  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
};

const writePerPage = (storageKey: string, value: number) => {
  safeSetItem(storageKey, String(value));
};

/** Falls back to `fallback` when nothing is stored or the stored value is unusable. */
export const readPersistentPerPage = (storageKey: string, fallback: number) => readPerPage(storageKey, fallback);

/** Must be rendered within a React-admin `<List>` for the pagination context. */
export const PerPagePersistence = ({ storageKey }: { storageKey: string }) => {
  const { perPage } = useListPaginationContext();

  useEffect(() => {
    if (perPage) {
      writePerPage(storageKey, perPage);
    }
  }, [perPage, storageKey]);

  return null;
};

export const __testing = {
  readPerPage,
  writePerPage,
};
