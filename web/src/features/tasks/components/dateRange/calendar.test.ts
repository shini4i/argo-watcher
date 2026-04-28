import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  PRESETS,
  buildMonthGrid,
  dateAt,
  dayCount,
  endOfDay,
  isSameDay,
  matchPreset,
  startOfDay,
} from './calendar';

const FIXED_NOW = new Date('2026-04-27T13:30:00Z');

describe('calendar helpers', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(FIXED_NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('startOfDay zeroes hours in UTC mode', () => {
    const date = startOfDay(new Date('2026-04-27T13:30:00Z'), 'utc');
    expect(date.toISOString()).toBe('2026-04-27T00:00:00.000Z');
  });

  it('endOfDay sets 23:59:59 in UTC mode', () => {
    const date = endOfDay(new Date('2026-04-27T13:30:00Z'), 'utc');
    expect(date.toISOString()).toBe('2026-04-27T23:59:59.000Z');
  });

  it('isSameDay compares calendar days only', () => {
    const left = new Date('2026-04-27T01:00:00Z');
    const right = new Date('2026-04-27T22:00:00Z');
    expect(isSameDay(left, right, 'utc')).toBe(true);
  });

  it('buildMonthGrid produces 42 cells starting on Monday', () => {
    const grid = buildMonthGrid(2026, 3, 'utc'); // April 2026 (month is 0-indexed)
    expect(grid).toHaveLength(42);
    // April 1, 2026 is a Wednesday → grid starts on Mon Mar 30 2026.
    expect(grid[0].date.toISOString().slice(0, 10)).toBe('2026-03-30');
    // The first cell that lands inside April should be index 2 (Wed Apr 1).
    expect(grid[2].date.toISOString().slice(0, 10)).toBe('2026-04-01');
    expect(grid[2].inMonth).toBe(true);
    expect(grid[0].inMonth).toBe(false);
  });

  it('flags today inside the grid', () => {
    const grid = buildMonthGrid(2026, 3, 'utc');
    const todayCell = grid.find(cell => cell.date.toISOString().startsWith('2026-04-27'));
    expect(todayCell?.isToday).toBe(true);
  });

  it('PRESETS computes Today as 00:00 → 23:59:59 in UTC', () => {
    const preset = PRESETS.find(p => p.id === 'today')!;
    const range = preset.compute('utc');
    expect(range.start).toBe(Math.floor(Date.parse('2026-04-27T00:00:00Z') / 1000));
    expect(range.end).toBe(Math.floor(Date.parse('2026-04-27T23:59:59Z') / 1000));
  });

  it('PRESETS computes Last 7 days as 7 calendar days ending today', () => {
    const preset = PRESETS.find(p => p.id === 'last-7d')!;
    const range = preset.compute('utc');
    expect(range.start).toBe(Math.floor(Date.parse('2026-04-21T00:00:00Z') / 1000));
    expect(range.end).toBe(Math.floor(Date.parse('2026-04-27T23:59:59Z') / 1000));
  });

  it('PRESETS computes This week (Mon→Sun)', () => {
    // 2026-04-27 is a Monday, so This week = Apr 27 → May 3.
    const preset = PRESETS.find(p => p.id === 'this-week')!;
    const range = preset.compute('utc');
    expect(range.start).toBe(Math.floor(Date.parse('2026-04-27T00:00:00Z') / 1000));
    expect(range.end).toBe(Math.floor(Date.parse('2026-05-03T23:59:59Z') / 1000));
  });

  it('matchPreset identifies the corresponding preset', () => {
    const today = PRESETS.find(p => p.id === 'today')!.compute('utc');
    expect(matchPreset(today, 'utc')).toBe('today');
    expect(matchPreset({ start: 1, end: 2 }, 'utc')).toBeNull();
  });

  it('dayCount returns inclusive day span', () => {
    const start = Math.floor(Date.parse('2026-04-21T00:00:00Z') / 1000);
    const end = Math.floor(Date.parse('2026-04-27T23:59:59Z') / 1000);
    expect(dayCount(start, end)).toBe(7);
  });

  it('dateAt builds midnight in UTC', () => {
    const date = dateAt(2026, 0, 1, 'utc');
    expect(date.toISOString()).toBe('2026-01-01T00:00:00.000Z');
  });
});
