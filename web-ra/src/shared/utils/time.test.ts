import { beforeEach, describe, expect, it, vi } from 'vitest';
import { formatDateTime, formatDuration, formatRelativeTime, relativeTimestamp } from './time';

describe('time utilities', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2025-01-01T00:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('formats date time with custom timezone', () => {
    expect(
      formatDateTime(1735689600, 'en-GB', {
        year: 'numeric',
        month: 'short',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        timeZone: 'UTC',
      }),
    ).toBe('01 Jan 2025, 00:00:00');
  });

  it('returns placeholder for invalid date', () => {
    expect(formatDateTime('invalid-date')).toBe('—');
  });

  it('formats duration in human readable units', () => {
    expect(formatDuration(30)).toBe('< 1 minute');
    expect(formatDuration(600)).toBe('10 minutes');
    expect(formatDuration(7200)).toBe('2 hours');
    expect(formatDuration(172800)).toBe('2 days');
    expect(formatDuration(3_110_400)).toBe('1 month');
    expect(formatDuration(63_000_000)).toBe('2 years');
  });

  it('formats relative time strings', () => {
    const timestamp = Date.parse('2024-12-31T23:59:00Z');
    expect(formatRelativeTime(timestamp)).toBe('1 minute ago');
  });

  it('computes relative timestamps', () => {
    expect(relativeTimestamp(60)).toBe(1735689540);
  });
});
