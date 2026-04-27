import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useListContext } from 'react-admin';
import { useSearchParams } from 'react-router-dom';
import { getBrowserWindow } from '../utils/browser';

export interface FilterFieldSchema<V> {
  /** Reads a URL search param (or null when missing) and returns the typed value. */
  readonly fromUrl: (raw: string | null) => V;
  /** Serialises a typed value back to a URL search param, or null to drop the param. */
  readonly toUrl: (value: V) => string | null;
  /** Writes the value into the react-admin filterValues object (key may differ). */
  readonly toFilter?: (value: V) => unknown;
  /** Optional override for the URL key (defaults to the schema key). */
  readonly urlKey?: string;
  /** Optional override for the filterValues key (defaults to the schema key). */
  readonly filterKey?: string;
  /** When true, persist values for this key under `${storageKey}.${storageField}`. */
  readonly storage?: boolean;
  /** Optional override for the localStorage field name (defaults to the schema key). */
  readonly storageField?: string;
}

export type FilterStateSchema<T> = {
  readonly [K in keyof T]: FilterFieldSchema<T[K]>;
};

interface FilterStateOptions<T> {
  readonly storageKey: string;
  readonly schema: FilterStateSchema<T>;
  readonly defaults: T;
}

interface FilterStateController<T> {
  readonly values: T;
  readonly applied: T;
  readonly isDirty: boolean;
  readonly setValue: <K extends keyof T>(key: K, value: T[K]) => void;
  readonly setMany: (next: Partial<T>) => void;
  readonly reset: () => void;
  /**
   * Mirrors values into URL + localStorage + react-admin filterValues.
   * Pass `override` to commit a specific snapshot (useful when you've just
   * called setValue/setMany and don't want to wait for state to flush).
   */
  readonly apply: (override?: T) => void;
}

const safeParse = <V>(field: FilterFieldSchema<V>, raw: string | null, fallback: V): V => {
  try {
    return field.fromUrl(raw);
  } catch {
    return fallback;
  }
};

const readStorageValue = <V>(
  storageKey: string,
  field: FilterFieldSchema<V>,
  schemaKey: string,
  fallback: V,
): V => {
  const storage = getBrowserWindow()?.localStorage;
  if (!storage || !field.storage) {
    return fallback;
  }
  const path = `${storageKey}.${field.storageField ?? schemaKey}`;
  const raw = storage.getItem(path);
  if (raw === null) {
    return fallback;
  }
  return safeParse(field, raw, fallback);
};

const writeStorageValue = <V>(
  storageKey: string,
  field: FilterFieldSchema<V>,
  schemaKey: string,
  value: V,
): void => {
  const storage = getBrowserWindow()?.localStorage;
  if (!storage || !field.storage) {
    return;
  }
  const path = `${storageKey}.${field.storageField ?? schemaKey}`;
  const serialised = field.toUrl(value);
  if (serialised === null || serialised === '') {
    storage.removeItem(path);
  } else {
    storage.setItem(path, serialised);
  }
};

const buildFilterValues = <T>(
  schema: FilterStateSchema<T>,
  values: T,
  base: Record<string, unknown>,
): Record<string, unknown> => {
  const next: Record<string, unknown> = { ...base };
  (Object.keys(schema) as Array<keyof T>).forEach(schemaKey => {
    const field = schema[schemaKey];
    const filterKey = field.filterKey ?? (schemaKey as string);
    const value = values[schemaKey];
    const projected = field.toFilter ? field.toFilter(value) : value;
    if (projected === undefined || projected === null || projected === '') {
      delete next[filterKey];
    } else {
      next[filterKey] = projected;
    }
  });
  return next;
};

const valuesEqual = <T>(a: T, b: T): boolean => {
  if (Object.is(a, b)) return true;
  if (typeof a !== 'object' || a === null || typeof b !== 'object' || b === null) return false;
  const aRecord = a as Record<string, unknown>;
  const bRecord = b as Record<string, unknown>;
  const keys = new Set([...Object.keys(aRecord), ...Object.keys(bRecord)]);
  for (const key of keys) {
    if (!Object.is(aRecord[key], bRecord[key])) return false;
  }
  return true;
};

/**
 * Single owner of URL ⇄ react-admin filterValues ⇄ localStorage reconciliation.
 *
 * On mount: reads URL params first, falls back to localStorage, then defaults.
 * Push the merged values into react-admin's filterValues.
 *
 * `setValue` / `setMany` update local state only — call `apply()` to mirror
 * pending values into the URL, storage, and filterValues. `applied` reflects
 * the values currently mirrored, so `isDirty` distinguishes pending edits.
 */
export const useFilterState = <T extends Record<string, unknown>>(
  options: FilterStateOptions<T>,
): FilterStateController<T> => {
  const { storageKey, schema, defaults } = options;
  const { filterValues = {}, setFilters } = useListContext();
  const [searchParams, setSearchParams] = useSearchParams();

  const initial = useMemo(() => {
    const next = { ...defaults };
    (Object.keys(schema) as Array<keyof T>).forEach(key => {
      const field = schema[key];
      const urlKey = field.urlKey ?? (key as string);
      const fromUrl = searchParams.get(urlKey);
      if (fromUrl !== null) {
        next[key] = safeParse(field, fromUrl, defaults[key]);
        return;
      }
      next[key] = readStorageValue(storageKey, field, key as string, defaults[key]);
    });
    return next;
    // We deliberately compute initial state once on mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const [values, setValues] = useState<T>(initial);
  const [applied, setApplied] = useState<T>(initial);

  // On first mount, project initial state into the react-admin filter context.
  const hasMountedRef = useRef(false);
  useEffect(() => {
    if (hasMountedRef.current) return;
    hasMountedRef.current = true;
    if (!setFilters) return;
    const merged = buildFilterValues(schema, initial, filterValues as Record<string, unknown>);
    setFilters(merged, {}, false);
    // We only do this once on mount — treat schema/initial/filterValues as stable inputs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const setValue = useCallback(<K extends keyof T>(key: K, value: T[K]) => {
    setValues(prev => ({ ...prev, [key]: value }));
  }, []);

  const setMany = useCallback((next: Partial<T>) => {
    setValues(prev => ({ ...prev, ...next }));
  }, []);

  const reset = useCallback(() => {
    setValues(defaults);
  }, [defaults]);

  const apply = useCallback(
    (override?: T) => {
      const target = override ?? values;

      // Mirror to react-admin filterValues.
      if (setFilters) {
        const merged = buildFilterValues(schema, target, filterValues as Record<string, unknown>);
        setFilters(merged, {}, false);
      }

      // Mirror to URL.
      const params = new URLSearchParams(searchParams);
      (Object.keys(schema) as Array<keyof T>).forEach(key => {
        const field = schema[key];
        const urlKey = field.urlKey ?? (key as string);
        const serialised = field.toUrl(target[key]);
        if (serialised === null || serialised === '') {
          params.delete(urlKey);
        } else {
          params.set(urlKey, serialised);
        }
      });
      setSearchParams(params, { replace: true });

      // Mirror to localStorage.
      (Object.keys(schema) as Array<keyof T>).forEach(key => {
        const field = schema[key];
        writeStorageValue(storageKey, field, key as string, target[key]);
      });

      if (override) {
        setValues(target);
      }
      setApplied(target);
    },
    [schema, storageKey, values, filterValues, setFilters, searchParams, setSearchParams],
  );

  const isDirty = useMemo(() => !valuesEqual(values, applied), [values, applied]);

  return { values, applied, isDirty, setValue, setMany, reset, apply };
};
