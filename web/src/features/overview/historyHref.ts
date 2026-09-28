import type { TimeRange } from './types';

/**
 * @description Builds the History link for one app over a range. History,
 * not Recent Tasks: Recent only reaches back 24 h, so a longer window would
 * open on an empty list.
 * @param app the application name
 * @param range the range, in unix seconds, the overview counted over
 * @returns a router path for `/history` filtered to the app and range
 */
export const historyHref = (app: string, range: TimeRange): string =>
  `/history?app=${encodeURIComponent(app)}&startDate=${range.start}&endDate=${range.end}`;
