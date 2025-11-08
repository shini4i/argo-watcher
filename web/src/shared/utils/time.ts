export const DEFAULT_DATE_FORMAT: Intl.DateTimeFormatOptions = {
  year: 'numeric',
  month: 'short',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
};

type SupportedTimestamp = Date | number | string | null | undefined;

/** Converts supported timestamp inputs into a Date instance, returning null when invalid. */
const toDate = (value: SupportedTimestamp): Date | null => {
  if (value === null || value === undefined) {
    return null;
  }

  if (value instanceof Date) {
    return value;
  }

  if (typeof value === 'number') {
    return new Date(value * (value < 10_000_000_000 ? 1000 : 1));
  }

  const parsed = Date.parse(value);
  if (Number.isNaN(parsed)) {
    return null;
  }

  return new Date(parsed);
};

/** Formats timestamps with locale-aware date+time options, defaulting to en-GB. */
export const formatDateTime = (
  value: SupportedTimestamp,
  locale: string | string[] = 'en-GB',
  options: Intl.DateTimeFormatOptions = DEFAULT_DATE_FORMAT,
) => {
  const date = toDate(value);
  if (!date) {
    return '—';
  }

  return new Intl.DateTimeFormat(locale, options).format(date);
};

const pluralize = (value: number, unit: string) => `${value} ${unit}${value === 1 ? '' : 's'}`;

/** Converts elapsed seconds into a human-readable relative duration string. */
export const formatDuration = (seconds: number): string => {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return '—';
  }

  if (seconds < 60) {
    return '< 1 minute';
  }

  if (seconds < 3600) {
    const minutes = Math.floor(seconds / 60);
    return pluralize(minutes, 'minute');
  }

  if (seconds < 86400) {
    const hours = Math.floor(seconds / 3600);
    return pluralize(hours, 'hour');
  }

  if (seconds < 2620800) {
    const days = Math.floor(seconds / 86400);
    return pluralize(days, 'day');
  }

  if (seconds < 31449600) {
    const months = Math.floor(seconds / 2620800);
    return pluralize(months, 'month');
  }

  const years = Math.floor(seconds / 31449600);
  return pluralize(years, 'year');
};

/** Formats timestamps as "X minutes ago" relative to now. */
export const formatRelativeTime = (value: SupportedTimestamp) => {
  const date = toDate(value);
  if (!date) {
    return '—';
  }

  const now = Date.now();
  const differenceSeconds = Math.max(0, Math.round((now - date.getTime()) / 1000));
  return `${formatDuration(differenceSeconds)} ago`;
};

/** Returns a UNIX timestamp offset by the provided seconds from the current moment. */
export const relativeTimestamp = (offsetSeconds: number) => {
  if (!Number.isFinite(offsetSeconds) || offsetSeconds < 0) {
    return Math.floor(Date.now() / 1000);
  }

  return Math.floor(Date.now() / 1000) - Math.floor(offsetSeconds);
};
