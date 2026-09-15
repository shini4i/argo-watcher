import { getBrowserWindow } from './browser';

// Safari private mode and a Firefox with cookies blocked throw SecurityError on
// any localStorage access — the property, not just the method — and a full quota
// throws QuotaExceededError on write. Every read falls back and every write is
// dropped, so a restricted browser loses only what the app remembered.

/**
 * @description Reads a localStorage value without letting a restricted browser
 * break the caller.
 * @param key full storage key
 * @returns the stored string, or null when absent or unreadable
 */
export const safeGetItem = (key: string): string | null => {
  try {
    return getBrowserWindow()?.localStorage?.getItem(key) ?? null;
  } catch {
    return null;
  }
};

/**
 * @description Stores a value, doing nothing when the browser refuses.
 * @param key full storage key
 * @param value string to store
 */
export const safeSetItem = (key: string, value: string): void => {
  try {
    getBrowserWindow()?.localStorage?.setItem(key, value);
  } catch {
    // Ignore — same restricted-storage rationale as safeGetItem.
  }
};

/**
 * @description Forgets a stored value, doing nothing when the browser refuses.
 * @param key full storage key
 */
export const safeRemoveItem = (key: string): void => {
  try {
    getBrowserWindow()?.localStorage?.removeItem(key);
  } catch {
    // Ignore — same restricted-storage rationale as safeGetItem.
  }
};
