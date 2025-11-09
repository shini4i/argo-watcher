import { Autocomplete, TextField } from '@mui/material';
import { useEffect, useState } from 'react';
import type { Task } from '../../../data/types';
import { getBrowserWindow } from '../../../shared/utils';

const DEFAULT_STORAGE_KEY = 'recentTasks.app';

/** Normalizes application filter inputs, collapsing null-like strings into empty values. */
export const normalizeApplicationFilterValue = (value?: string | null): string => {
  if (typeof value !== 'string') {
    return '';
  }

  const trimmed = value.trim();
  if (!trimmed || trimmed.toLowerCase() === 'null') {
    return '';
  }

  return value;
};

/** Reads the persisted application filter (if any) from localStorage. */
const readStoredApp = (storageKey: string) =>
  normalizeApplicationFilterValue(getBrowserWindow()?.localStorage?.getItem(storageKey));

/**
 * Autocomplete component used to filter tasks by application name.
 */
export const ApplicationFilter = ({
  records,
  value,
  onChange,
  storageKey = DEFAULT_STORAGE_KEY,
}: {
  records: readonly Task[];
  value: string;
  onChange: (next: string) => void;
  storageKey?: string;
}) => {
  const [options, setOptions] = useState<string[]>([]);

  useEffect(() => {
    const unique = Array.from(
      new Set(
        records.map(record => normalizeApplicationFilterValue(record.app)).filter(Boolean),
      ),
    ).sort((a, b) => a.localeCompare(b));
    setOptions(unique);
  }, [records]);

  return (
    <Autocomplete
      size="small"
      options={options}
      value={value}
      onChange={(_event, newValue = '') => {
        const next = normalizeApplicationFilterValue(newValue);
        const storage = getBrowserWindow()?.localStorage;
        if (next) {
          storage?.setItem(storageKey, next);
        } else {
          storage?.removeItem(storageKey);
        }
        onChange(next);
      }}
      renderInput={params => <TextField {...params} label="Application" placeholder="Filter" />}
      clearOnBlur={false}
      freeSolo
    />
  );
};

/** Convenience helper for initializing filters from localStorage. */
export const readInitialApplication = (storageKey: string = DEFAULT_STORAGE_KEY): string =>
  readStoredApp(storageKey);
