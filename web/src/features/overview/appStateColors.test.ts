import { describe, expect, it } from 'vitest';
import { appStateColors } from './appStateColors';
import { deriveAppState } from './deriveOverview';
import { tokens } from '../../theme/tokens';
import type { AppSummary } from './types';

const summary = (over: Partial<AppSummary>): AppSummary => ({
  app: 'demo',
  project: 'acme',
  total: 10,
  deployed: 9,
  failed: 0,
  running: 0,
  last_status: 'deployed',
  last_created: 0,
  last_status_reason: '',
  median_duration_seconds: 0,
  recent_statuses: [],
  ...over,
});

describe('appStateColors', () => {
  it('paints a failing app in the failed palette', () => {
    expect(appStateColors('failing', false).fg).toBe(tokens.statusFailedFg);
    expect(appStateColors('failing', true).fg).toBe(tokens.statusFailedFgDark);
  });

  it('paints a deployed app in the deployed palette', () => {
    expect(appStateColors('deployed', false).fg).toBe(tokens.statusDeployedFg);
  });

  // The badge said "Failing" while wearing the success green, because its colour
  // came from last_status and its text from the derived state.
  it('never paints a failing app green, whatever its last status was', () => {
    const recovered = summary({ failed: 3, last_status: 'deployed' });

    expect(deriveAppState(recovered)).toBe('failing');
    for (const isDark of [false, true]) {
      const colors = appStateColors(deriveAppState(recovered), isDark);
      expect(colors.fg).not.toBe(isDark ? tokens.statusDeployedFgDark : tokens.statusDeployedFg);
      expect(colors.fg).toBe(isDark ? tokens.statusFailedFgDark : tokens.statusFailedFg);
    }
  });

  it('gives every state a defined pair in both modes', () => {
    for (const state of ['failing', 'running', 'deployed', 'idle'] as const) {
      for (const isDark of [false, true]) {
        const colors = appStateColors(state, isDark);
        expect(colors.fg).toBeTruthy();
        expect(colors.bg).toBeTruthy();
      }
    }
  });
});
