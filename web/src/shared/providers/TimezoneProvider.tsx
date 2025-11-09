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
  const browserWindow = globalThis.window;
  if (!browserWindow) {
    return 'utc';
  }
  const stored = browserWindow.localStorage.getItem(STORAGE_KEY);
  if (stored === 'local' || stored === 'utc') {
    return stored;
  }
  return 'utc';
};

/** React context provider that exposes timezone selection and formatting helpers. */
export const TimezoneProvider = ({ children }: { children: ReactNode }) => {
  const [timezone, setTimezone] = useState<TimezoneMode>(() => readInitialTimezone());

  const persistTimezone = useCallback((mode: TimezoneMode) => {
    setTimezone(mode);
    const browserWindow = globalThis.window;
    if (browserWindow) {
      browserWindow.localStorage.setItem(STORAGE_KEY, mode);
    }
  }, []);

  const formatDate = useCallback(
    (value: Parameters<typeof formatDateTime>[0], options: Intl.DateTimeFormatOptions = {}) => {
      const baseOptions = { ...DEFAULT_DATE_FORMAT, ...options };
      const finalOptions = timezone === 'utc' ? { ...baseOptions, timeZone: 'UTC' } : baseOptions;
      return formatDateTime(value, undefined, finalOptions);
    },
    [timezone],
  );

  const value = useMemo(
    () => ({
      timezone,
      setTimezone: persistTimezone,
      formatDate,
    }),
    [timezone, persistTimezone, formatDate],
  );

  return <TimezoneContext.Provider value={value}>{children}</TimezoneContext.Provider>;
};

/** Hook for consuming the timezone context (selection + formatter). */
export const useTimezone = () => {
  const context = useContext(TimezoneContext);
  if (!context) {
    return {
      timezone: 'utc' as TimezoneMode,
      setTimezone: () => {
        if (import.meta.env.DEV) {
          console.warn('useTimezone fallback: provider missing; defaulting to UTC.');
        }
      },
      formatDate: (value: Parameters<typeof formatDateTime>[0], options: Intl.DateTimeFormatOptions = {}) =>
        formatDateTime(value, undefined, { ...DEFAULT_DATE_FORMAT, ...options, timeZone: 'UTC' }),
    } satisfies TimezoneContextValue;
  }
  return context;
};
