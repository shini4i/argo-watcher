import { isFailedStatus, isRunningStatus } from '../tasks/utils/statusPresentation';
import type { AppSummary } from './types';

export type AppState = 'failing' | 'running' | 'deployed' | 'idle';

export interface OverviewKpis {
  readonly runningNow: number;
  readonly failed: number;
  readonly deployed: number;
  /** Median of the per-app medians; 0 when no app has a settled task. */
  readonly medianDurationSeconds: number;
}

/** An app needing attention comes before one that is merely busy. */
const STATE_RANK: Readonly<Record<AppState, number>> = {
  failing: 0,
  running: 1,
  deployed: 2,
  idle: 3,
};

/**
 * @description Classifies an app by what a reader should do about it: a failure
 * in the window outranks an in-flight deployment, which outranks a clean one.
 * @param summary the app's aggregate for the window
 * @returns the state used for ordering and colour
 */
export const deriveAppState = (summary: AppSummary): AppState => {
  if (summary.failed > 0 || isFailedStatus(summary.last_status)) {
    return 'failing';
  }
  if (summary.running > 0 || isRunningStatus(summary.last_status)) {
    return 'running';
  }
  // A real deployment is required to call an app deployed. Counting any task
  // rendered `app not found` — which never deploys anything — as Healthy, a
  // false all-clear on the surface built to surface trouble.
  if (summary.deployed > 0 || summary.last_status === 'deployed') {
    return 'deployed';
  }
  return 'idle';
};

/** True when the app has something a reader may need to act on. */
export const needsAttention = (summary: AppSummary): boolean => {
  const state = deriveAppState(summary);
  return state === 'failing' || state === 'running';
};

const median = (values: readonly number[]): number => {
  if (values.length === 0) {
    return 0;
  }
  const sorted = [...values].sort((a, b) => a - b);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 1 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
};

/**
 * @description Totals the window across every application. The values are sums
 * of exact per-app counts, so they are exact too.
 * @param summaries every app in the window
 * @returns the four headline numbers
 */
export const deriveKpis = (summaries: readonly AppSummary[]): OverviewKpis => {
  const durations = summaries
    .filter(summary => summary.median_duration_seconds > 0)
    .map(summary => summary.median_duration_seconds);

  return {
    runningNow: summaries.reduce((sum, summary) => sum + summary.running, 0),
    failed: summaries.reduce((sum, summary) => sum + summary.failed, 0),
    deployed: summaries.reduce((sum, summary) => sum + summary.deployed, 0),
    medianDurationSeconds: median(durations),
  };
};

/**
 * @description Orders apps failing → running → recently deployed, and by most
 * recent activity within a state, so the top of the list is what to look at.
 * @param summaries the apps to order
 * @returns a new, sorted array
 */
export const rankByAttention = (summaries: readonly AppSummary[]): AppSummary[] =>
  [...summaries].sort((a, b) => {
    const byState = STATE_RANK[deriveAppState(a)] - STATE_RANK[deriveAppState(b)];
    if (byState !== 0) {
      return byState;
    }
    return b.last_created - a.last_created;
  });
