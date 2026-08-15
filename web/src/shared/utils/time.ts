export const DEFAULT_DATE_FORMAT: Intl.DateTimeFormatOptions = {
  year: 'numeric',
  month: 'short',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
};

type SupportedTimestamp = Date | number | string | null | undefined;

const toDate = (value: SupportedTimestamp): Date | null => {
  if (value === null || value === undefined) {
    return null;
  }

  if (value instanceof Date) {
    return value;
  }

  if (typeof value === 'number') {
    // Unix timestamps stay below 10_000_000_000 seconds until the year 2286, so
    // values under that threshold are interpreted as seconds instead of ms.
    const normalized = value < 10_000_000_000 ? value * 1000 : value;
    return new Date(normalized);
  }

  const parsed = Date.parse(value);
  if (Number.isNaN(parsed)) {
    return null;
  }

  return new Date(parsed);
};

/** Returns "—" when the value cannot be parsed as a timestamp. */
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

/** Prose form ("2 minutes"); "—" for a negative or non-finite input. */
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

/**
 * Dense monospace form ("1m 04s", "2h 03m", "—") for table cells, where vertical
 * alignment matters. `formatDuration` is kept for prose contexts (status
 * reasons, detail pages) that read better as "2 minutes" than as "2m 00s".
 */
export const formatDurationCompact = (seconds: number): string => {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return '—';
  }

  const total = Math.floor(seconds);
  const pad = (value: number) => value.toString().padStart(2, '0');

  if (total < 60) {
    return `${total}s`;
  }
  if (total < 3600) {
    const minutes = Math.floor(total / 60);
    const remSeconds = total % 60;
    return `${minutes}m ${pad(remSeconds)}s`;
  }
  if (total < 86400) {
    const hours = Math.floor(total / 3600);
    const remMinutes = Math.floor((total % 3600) / 60);
    return `${hours}h ${pad(remMinutes)}m`;
  }
  const days = Math.floor(total / 86400);
  const remHours = Math.floor((total % 86400) / 3600);
  return `${days}d ${pad(remHours)}h`;
};

/** "<duration> ago", never negative; "—" when the value cannot be parsed. */
export const formatRelativeTime = (value: SupportedTimestamp) => {
  const date = toDate(value);
  if (!date) {
    return '—';
  }

  const now = Date.now();
  const differenceSeconds = Math.max(0, Math.round((now - date.getTime()) / 1000));
  return `${formatDuration(differenceSeconds)} ago`;
};

/** UNIX seconds `offsetSeconds` in the past; a negative or non-finite offset yields now. */
export const relativeTimestamp = (offsetSeconds: number) => {
  if (!Number.isFinite(offsetSeconds) || offsetSeconds < 0) {
    return Math.floor(Date.now() / 1000);
  }

  return Math.floor(Date.now() / 1000) - Math.floor(offsetSeconds);
};
