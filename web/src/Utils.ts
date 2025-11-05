/**
 * Safely resolves the browser window object from the universal global scope.
 *
 * @returns The Window instance when running in a browser environment, otherwise undefined.
 */
export const resolveBrowserWindow = (): Window | undefined => {
  if (typeof globalThis !== 'object' || globalThis === null) {
    return undefined;
  }

  if ('window' in globalThis && (globalThis as typeof globalThis & { window: Window }).window) {
    return (globalThis as typeof globalThis & { window: Window }).window;
  }

  if ('location' in globalThis) {
    return globalThis as unknown as Window;
  }

  return undefined;
};

/**
 * Returns the relative time string (e.g. "5 minutes ago") for the supplied timestamp.
 *
 * @param oldTimestamp Unix timestamp in milliseconds.
 * @returns Human readable relative time description.
 */
export const relativeTime = (oldTimestamp: number): string => {
  const timestamp = Date.now();
  const difference = Math.round(timestamp / 1000 - oldTimestamp / 1000);
  if (oldTimestamp === 0) {
    return '-';
  }
  return relativeHumanDuration(difference) + ' ago';
};

/**
 * Produces an "s" suffix when the provided number is greater than one to help pluralise units.
 *
 * @param value Numeric value to evaluate.
 * @returns "s" when plural, otherwise an empty string.
 */
const numberEnding = (value: number): string => (value > 1 ? 's' : '');

/**
 * Formats a duration represented in seconds into a human readable string.
 *
 * @param seconds Duration in seconds.
 * @returns Human readable duration string.
 */
export const relativeHumanDuration = (seconds: number): string => {
  if (seconds < 60) {
    // Less than a minute has passed:
    return `< 1 minute`;
  } else if (seconds < 3600) {
    // Less than an hour has passed:
    const minutes = Math.floor(seconds / 60);
    return `${minutes} minute${numberEnding(minutes)}`;
  } else if (seconds < 86400) {
    // Less than a day has passed:
    const hours = Math.floor(seconds / 3600);
    return `${hours} hour${numberEnding(hours)}`;
  } else if (seconds < 2620800) {
    // Less than a month has passed:
    const days = Math.floor(seconds / 86400);
    return `${days} day${numberEnding(days)}`;
  } else if (seconds < 31449600) {
    // Less than a year has passed:
    const months = Math.floor(seconds / 2620800);
    return `${months} month${numberEnding(months)}`;
  }

  // More than a year has passed:
  return `${Math.floor(seconds / 31449600)} years`;
};

/**
 * Computes the timestamp (seconds) relative to the current time given a timeframe in seconds.
 *
 * @param timeframe Number of seconds to subtract from now.
 * @returns The Unix timestamp representing the start of the timeframe.
 */
export const relativeTimestamp = (timeframe: number): number => {
  return Math.floor(Date.now() / 1000) - timeframe;
};
