import { describe, expect, it } from 'vitest';
import { deriveAppState, deriveKpis, needsAttention, rankByAttention } from './deriveOverview';
import type { AppSummary } from './types';

const summary = (overrides: Partial<AppSummary> = {}): AppSummary => ({
  app: 'checkout',
  project: 'acme/checkout',
  total: 1,
  failed: 0,
  running: 0,
  deployed: 1,
  median_duration_seconds: 30,
  last_created: 1000,
  last_status: 'deployed',
  recent_statuses: ['deployed'],
  ...overrides,
});

describe('deriveAppState', () => {
  it('calls an app failing when the window holds a failure', () => {
    expect(deriveAppState(summary({ failed: 1, last_status: 'deployed' }))).toBe('failing');
  });

  // The newest task is what an on-call reader is reacting to, even when the
  // window's counters have not caught up with it.
  it('calls an app failing when its newest task failed', () => {
    expect(deriveAppState(summary({ failed: 0, last_status: 'aborted' }))).toBe('failing');
  });

  it('prefers failing over running when both are true', () => {
    expect(deriveAppState(summary({ failed: 1, running: 1 }))).toBe('failing');
  });

  it('calls an app running when a deployment is in flight', () => {
    expect(deriveAppState(summary({ running: 1, last_status: 'in progress' }))).toBe('running');
  });

  it('calls a clean app deployed', () => {
    expect(deriveAppState(summary())).toBe('deployed');
  });

  it('calls an app with no tasks in the window idle', () => {
    expect(deriveAppState(summary({ total: 0, deployed: 0, last_status: '' }))).toBe('idle');
  });
});

describe('needsAttention', () => {
  it('includes failing and running apps only', () => {
    expect(needsAttention(summary({ failed: 1 }))).toBe(true);
    expect(needsAttention(summary({ running: 1, last_status: 'in progress' }))).toBe(true);
    expect(needsAttention(summary())).toBe(false);
    expect(needsAttention(summary({ total: 0, deployed: 0, last_status: '' }))).toBe(false);
  });
});

describe('deriveKpis', () => {
  it('sums the per-app counters', () => {
    const kpis = deriveKpis([
      summary({ app: 'a', running: 2, failed: 1, deployed: 5 }),
      summary({ app: 'b', running: 1, failed: 3, deployed: 2 }),
    ]);

    expect(kpis.runningNow).toBe(3);
    expect(kpis.failed).toBe(4);
    expect(kpis.deployed).toBe(7);
  });

  it('takes the median of the per-app medians', () => {
    const kpis = deriveKpis([
      summary({ app: 'a', median_duration_seconds: 10 }),
      summary({ app: 'b', median_duration_seconds: 50 }),
      summary({ app: 'c', median_duration_seconds: 30 }),
    ]);

    expect(kpis.medianDurationSeconds).toBe(30);
  });

  // An app with nothing settled reports 0; averaging that in would understate
  // every other app's duration.
  it('ignores apps with no settled duration', () => {
    const kpis = deriveKpis([
      summary({ app: 'a', median_duration_seconds: 0 }),
      summary({ app: 'b', median_duration_seconds: 40 }),
    ]);

    expect(kpis.medianDurationSeconds).toBe(40);
  });

  it('reports zero duration when nothing has settled anywhere', () => {
    expect(deriveKpis([summary({ median_duration_seconds: 0 })]).medianDurationSeconds).toBe(0);
  });

  it('handles an empty window without dividing by zero', () => {
    expect(deriveKpis([])).toEqual({
      runningNow: 0,
      failed: 0,
      deployed: 0,
      medianDurationSeconds: 0,
    });
  });
});

describe('rankByAttention', () => {
  it('orders failing, then running, then deployed', () => {
    const ranked = rankByAttention([
      summary({ app: 'clean' }),
      summary({ app: 'busy', running: 1, last_status: 'in progress' }),
      summary({ app: 'broken', failed: 1 }),
    ]);

    expect(ranked.map(entry => entry.app)).toEqual(['broken', 'busy', 'clean']);
  });

  it('puts the most recent activity first within a state', () => {
    const ranked = rankByAttention([
      summary({ app: 'older', failed: 1, last_created: 100 }),
      summary({ app: 'newer', failed: 1, last_created: 900 }),
    ]);

    expect(ranked.map(entry => entry.app)).toEqual(['newer', 'older']);
  });

  it('does not mutate its input', () => {
    const input = [summary({ app: 'clean' }), summary({ app: 'broken', failed: 1 })];
    rankByAttention(input);
    expect(input.map(entry => entry.app)).toEqual(['clean', 'broken']);
  });
});

// An app whose deployment never landed must not read as an all-clear on the
// page built to surface trouble.
describe('deriveAppState never reports a false all-clear', () => {
  it('does not call an app deployed when nothing deployed', () => {
    const state = deriveAppState(
      summary({ total: 1, deployed: 0, failed: 0, running: 0, last_status: 'app not found' }),
    );
    expect(state).not.toBe('deployed');
    expect(state).toBe('idle');
  });

  it('does not call a cancelled-only app deployed', () => {
    expect(
      deriveAppState(summary({ total: 1, deployed: 0, last_status: 'cancelled' })),
    ).toBe('idle');
  });

  it('does not call an unrecognised terminal status deployed', () => {
    expect(
      deriveAppState(summary({ total: 1, deployed: 0, last_status: 'something new' })),
    ).toBe('idle');
  });

  it('still calls a genuinely deployed app deployed', () => {
    expect(deriveAppState(summary({ total: 1, deployed: 1, last_status: 'deployed' }))).toBe('deployed');
  });

  // The window's counters can lag the newest task, so either signal suffices.
  it('trusts the newest status when the counter has not caught up', () => {
    expect(deriveAppState(summary({ total: 1, deployed: 0, last_status: 'deployed' }))).toBe('deployed');
  });

  it('keeps an app-not-found app out of the deployed KPI', () => {
    const kpis = deriveKpis([summary({ total: 1, deployed: 0, failed: 0, last_status: 'app not found' })]);
    expect(kpis.deployed).toBe(0);
  });
});
