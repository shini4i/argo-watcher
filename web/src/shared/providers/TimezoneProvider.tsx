import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';
import { DEFAULT_DATE_FORMAT, formatDateTime } from '../utils/time';

export type TimezoneMode = 'local' | 'utc';

interface TimezoneContextValue {
  timezone: TimezoneMode;
  setTimezone: (mode: TimezoneMode) => void;
  formatDate: (value: Parameters<typeof formatDateTime>[0], options?: Intl.DateTimeFormatOptions) => string;
}

const STORAGE_KEY = 'argo-watcher:timezone';

const TimezoneContext = createContext<TimezoneContextValue | undefined>(undefined);

/** Reads the persisted timezone selection, defaulting to UTC when unset. */
const readInitialTimezone = (): TimezoneMode => {
  if (typeof window === 'undefined') {
    return 'utc';
  }
  const stored = window.localStorage.getItem(STORAGE_KEY);
  if (stored === 'local' || stored === 'utc') {
    return stored;
  }
  return 'utc';
};

/** React context provider that exposes timezone selection and formatting helpers. */
export const TimezoneProvider = ({ children }: { children: ReactNode }) => {
  const [timezone, setTimezoneState] = useState<TimezoneMode>(readInitialTimezone);

  const setTimezone = useCallback((mode: TimezoneMode) => {
    setTimezoneState(mode);
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(STORAGE_KEY, mode);
    }
  }, []);

  const formatDate = useCallback(
    (value: Parameters<typeof formatDateTime>[0], options?: Intl.DateTimeFormatOptions) => {
      const baseOptions = { ...DEFAULT_DATE_FORMAT, ...(options ?? {}) };
      const finalOptions = timezone === 'utc' ? { ...baseOptions, timeZone: 'UTC' } : baseOptions;
      return formatDateTime(value, undefined, finalOptions);
    },
    [timezone],
  );

  const value = useMemo(
    () => ({
      timezone,
      setTimezone,
      formatDate,
    }),
    [timezone, setTimezone, formatDate],
  );

  return <TimezoneContext.Provider value={value}>{children}</TimezoneContext.Provider>;
};

/** Hook for consuming the timezone context (selection + formatter). */
export const useTimezone = () => {
  const context = useContext(TimezoneContext);
  if (!context) {
    throw new Error('useTimezone must be used within a TimezoneProvider');
  }
  return context;
};
