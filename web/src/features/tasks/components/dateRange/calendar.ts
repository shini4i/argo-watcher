import type { TimezoneMode } from '../../../../shared/providers/TimezoneProvider';

export interface DateRangeValue {
  /** Unix seconds for the range start (00:00 of the chosen day in the active timezone). */
  readonly start: number | null;
  /** Unix seconds for the range end (23:59:59 of the chosen day in the active timezone). */
  readonly end: number | null;
}

export interface CalendarDay {
  readonly date: Date;
  readonly inMonth: boolean;
  readonly isToday: boolean;
}

const MS_PER_SECOND = 1000;

/** Returns the user's "now" as seconds, regardless of timezone. */
const nowSeconds = (): number => Math.floor(Date.now() / MS_PER_SECOND);

/** Returns 0..6 with Monday=0, Sunday=6 from a Date's weekday. */
const mondayIndex = (date: Date, mode: TimezoneMode): number => {
  const day = mode === 'utc' ? date.getUTCDay() : date.getDay();
  return (day + 6) % 7;
};

/** Returns a new Date set to 00:00:00.000 in the given timezone mode. */
export const startOfDay = (input: Date, mode: TimezoneMode): Date => {
  const date = new Date(input);
  if (mode === 'utc') {
    date.setUTCHours(0, 0, 0, 0);
  } else {
    date.setHours(0, 0, 0, 0);
  }
  return date;
};

/** Returns a new Date set to 23:59:59.000 in the given timezone mode. */
export const endOfDay = (input: Date, mode: TimezoneMode): Date => {
  const date = new Date(input);
  if (mode === 'utc') {
    date.setUTCHours(23, 59, 59, 0);
  } else {
    date.setHours(23, 59, 59, 0);
  }
  return date;
};

/** Returns the year, month (0-11) and day-of-month for the given Date in the active timezone. */
export const ymd = (date: Date, mode: TimezoneMode): { year: number; month: number; day: number } => {
  if (mode === 'utc') {
    return { year: date.getUTCFullYear(), month: date.getUTCMonth(), day: date.getUTCDate() };
  }
  return { year: date.getFullYear(), month: date.getMonth(), day: date.getDate() };
};

/** Builds a Date object representing midnight (in `mode`) for the given calendar coordinates. */
export const dateAt = (year: number, month: number, day: number, mode: TimezoneMode): Date => {
  if (mode === 'utc') {
    return new Date(Date.UTC(year, month, day, 0, 0, 0));
  }
  return new Date(year, month, day, 0, 0, 0);
};

/**
 * Advances a date by `days` calendar days in the active timezone. Uses
 * setDate / setUTCDate (rather than millisecond arithmetic) so DST
 * transitions don't shift the wall-clock time by ±1 hour.
 */
export const addDays = (date: Date, days: number, mode: TimezoneMode): Date => {
  const next = new Date(date);
  if (mode === 'utc') {
    next.setUTCDate(next.getUTCDate() + days);
  } else {
    next.setDate(next.getDate() + days);
  }
  return next;
};

/** Returns true when two dates resolve to the same day in `mode`. */
export const isSameDay = (a: Date, b: Date, mode: TimezoneMode): boolean => {
  const left = ymd(a, mode);
  const right = ymd(b, mode);
  return left.year === right.year && left.month === right.month && left.day === right.day;
};

/** Builds a 6×7 grid (Monday-first) of CalendarDay entries for the given month. */
export const buildMonthGrid = (year: number, month: number, mode: TimezoneMode): CalendarDay[] => {
  const firstOfMonth = dateAt(year, month, 1, mode);
  const offset = mondayIndex(firstOfMonth, mode);
  const start = addDays(firstOfMonth, -offset, mode);
  const today = new Date();
  const cells: CalendarDay[] = [];
  for (let i = 0; i < 42; i += 1) {
    const date = addDays(start, i, mode);
    const inMonth = ymd(date, mode).month === month;
    cells.push({
      date,
      inMonth,
      isToday: isSameDay(date, today, mode),
    });
  }
  return cells;
};

export interface PresetDescriptor {
  readonly id: string;
  readonly label: string;
  readonly compute: (mode: TimezoneMode) => DateRangeValue;
}

/** Converts a Date into Unix seconds. */
const toSeconds = (date: Date): number => Math.floor(date.getTime() / MS_PER_SECOND);

const today = (mode: TimezoneMode): DateRangeValue => {
  const now = new Date();
  return { start: toSeconds(startOfDay(now, mode)), end: toSeconds(endOfDay(now, mode)) };
};

const yesterday = (mode: TimezoneMode): DateRangeValue => {
  const ref = addDays(new Date(), -1, mode);
  return { start: toSeconds(startOfDay(ref, mode)), end: toSeconds(endOfDay(ref, mode)) };
};

const last24Hours = (): DateRangeValue => {
  const end = nowSeconds();
  return { start: end - 24 * 60 * 60, end };
};

const lastNDays = (n: number, mode: TimezoneMode): DateRangeValue => {
  const todayDate = startOfDay(new Date(), mode);
  return {
    start: toSeconds(addDays(todayDate, -(n - 1), mode)),
    end: toSeconds(endOfDay(new Date(), mode)),
  };
};

const thisWeek = (mode: TimezoneMode): DateRangeValue => {
  const now = new Date();
  const offset = mondayIndex(now, mode);
  const monday = startOfDay(addDays(now, -offset, mode), mode);
  const sunday = endOfDay(addDays(monday, 6, mode), mode);
  return { start: toSeconds(monday), end: toSeconds(sunday) };
};

const thisMonth = (mode: TimezoneMode): DateRangeValue => {
  const now = new Date();
  const { year, month } = ymd(now, mode);
  const first = dateAt(year, month, 1, mode);
  const lastDayOfMonth = addDays(dateAt(year, month + 1, 1, mode), -1, mode);
  return { start: toSeconds(first), end: toSeconds(endOfDay(lastDayOfMonth, mode)) };
};

export const PRESETS: ReadonlyArray<PresetDescriptor> = [
  { id: 'today', label: 'Today', compute: today },
  { id: 'yesterday', label: 'Yesterday', compute: yesterday },
  { id: 'last-24h', label: 'Last 24 hours', compute: () => last24Hours() },
  { id: 'last-7d', label: 'Last 7 days', compute: mode => lastNDays(7, mode) },
  { id: 'last-30d', label: 'Last 30 days', compute: mode => lastNDays(30, mode) },
  { id: 'this-week', label: 'This week', compute: thisWeek },
  { id: 'this-month', label: 'This month', compute: thisMonth },
];

/** Identifies the preset that matches the given range (or null when no preset matches). */
export const matchPreset = (value: DateRangeValue, mode: TimezoneMode): string | null => {
  if (value.start === null || value.end === null) return null;
  for (const preset of PRESETS) {
    const computed = preset.compute(mode);
    if (computed.start === value.start && computed.end === value.end) {
      return preset.id;
    }
  }
  return null;
};

/** Counts the inclusive number of days between two timestamps in seconds. */
export const dayCount = (start: number, end: number): number => {
  if (end < start) return 0;
  return Math.max(1, Math.round((end - start) / (60 * 60 * 24)));
};
