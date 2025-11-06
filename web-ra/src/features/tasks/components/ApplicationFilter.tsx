import { Autocomplete, TextField } from '@mui/material';
import { useEffect, useState } from 'react';
import type { Task } from '../../../data/types';

const DEFAULT_STORAGE_KEY = 'recentTasks.app';

const readStoredApp = (storageKey: string) => {
  if (typeof window === 'undefined') {
    return '';
  }
  return window.localStorage.getItem(storageKey) ?? '';
};

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
    const unique = Array.from(new Set(records.map(record => record.app))).sort((a, b) =>
      a.localeCompare(b),
    );
    setOptions(unique);
  }, [records]);

  return (
    <Autocomplete
      size="small"
      options={options}
      value={value}
      onChange={(_event, newValue) => {
        const next = newValue ?? '';
        window.localStorage.setItem(storageKey, next);
        onChange(next);
      }}
      renderInput={params => <TextField {...params} label="Application" placeholder="Filter" />}
      clearOnBlur={false}
      freeSolo
    />
  );
};

export const readInitialApplication = (storageKey: string = DEFAULT_STORAGE_KEY): string =>
  readStoredApp(storageKey);
