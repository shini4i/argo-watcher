import { describe, expect, it } from 'vitest';
import { summariseFailure } from './failureReason';

/**
 * @description Real `status_reason` shapes, generated from the Go that composes
 * them: argocd.rolloutFailureHeadline + rolloutMessage,
 * ImageNotPartOfAppError.Reason, argocd.ArgoAPIErrorTemplate and
 * state.StaleTaskAbortReason. Regenerate from those functions if the backend's
 * wording changes — hand-invented Argo CD text is not evidence.
 */
const REAL = {
  notAvailable: [
    'Application deployment failed. Rollout status is not available',
    '',
    'List of current images (last app check):',
    '\tapp:v0.0.1',
    '',
    'List of expected images:',
    '\tapp:v0.0.2',
    '',
    'Sync operation phase: Failed',
    'Sync operation message: one or more synchronization tasks completed unsuccessfully',
    '',
    'Unhealthy resources:',
    '\tPod(app-xyz) Degraded with message Back-off pulling image "app:v0.0.2": ErrImagePull',
  ].join('\n'),

  notHealthy: [
    'Application deployment failed. Rollout status is not healthy',
    '',
    'App sync status "Synced"',
    'App health status "Progressing"',
    'Resources:',
    '\tDeployment(app) Degraded with message replicas unavailable',
  ].join('\n'),

  notSynced: [
    'Deployment failed: ArgoCD reports sync status OutOfSync after waiting 1m37s.',
    '',
    'Unhealthy resources:',
    '\tPod(app-xyz) Degraded with message Back-off pulling image "app:v0.0.2": ErrImagePull',
  ].join('\n'),

  apiError: 'ArgoCD API Error: applications.argoproj.io "checkout" not found',

  imageNotPartOfApp: [
    'Application deployment failed. Image "app:v2" is not part of application "checkout".',
    '',
    'List of images defined in the application:',
    '\tapp:v1',
  ].join('\n'),

  staleAbort:
    'Deployment did not complete within the staleness window; marked aborted by argo-watcher.',

  // The shape that made a body search wrong: Argo CD's own sync message embeds
  // "Error:", and hoisting it discarded the headline the backend composed.
  innerErrorColon: [
    'Application deployment failed. Rollout status is not available',
    '',
    'Sync operation phase: Failed',
    'Sync operation message: one or more objects failed to apply, reason: Error: UPGRADE FAILED: timed out',
    '',
    'Unhealthy resources:',
    '\tPod(app-1) Degraded',
  ].join('\n'),
};

describe('summariseFailure on real backend reasons', () => {
  it('returns null when there is no reason', () => {
    expect(summariseFailure(undefined)).toBeNull();
    expect(summariseFailure(null)).toBeNull();
    expect(summariseFailure('   ')).toBeNull();
  });

  it.each([
    ['not available', REAL.notAvailable, 'Application deployment failed. Rollout status is not available'],
    ['not healthy', REAL.notHealthy, 'Application deployment failed. Rollout status is not healthy'],
    ['not synced', REAL.notSynced, 'Deployment failed: ArgoCD reports sync status OutOfSync after waiting 1m37s.'],
    [
      'image not part of app',
      REAL.imageNotPartOfApp,
      'Application deployment failed. Image "app:v2" is not part of application "checkout".',
    ],
    ['stale abort', REAL.staleAbort, REAL.staleAbort],
  ])('uses the backend headline for a %s failure', (_name, raw, expected) => {
    expect(summariseFailure(raw)!.headline).toBe(expected);
  });

  // The regression this rewrite exists for.
  it('keeps the composed headline when the body embeds "Error:"', () => {
    const summary = summariseFailure(REAL.innerErrorColon)!;

    expect(summary.headline).toBe('Application deployment failed. Rollout status is not available');
    expect(summary.headline).not.toContain('UPGRADE FAILED');
  });

  it('strips the ArgoCD API Error prefix, which the panel already conveys', () => {
    expect(summariseFailure(REAL.apiError)!.headline).toBe(
      'applications.argoproj.io "checkout" not found',
    );
  });

  it('keeps a headline when the reason is only that prefix', () => {
    expect(summariseFailure('ArgoCD API Error:')!.headline).toBe('ArgoCD API Error:');
  });

  it('never modifies the raw reason, whatever the headline', () => {
    for (const raw of Object.values(REAL)) {
      expect(summariseFailure(raw)!.raw).toBe(raw);
    }
  });

  it('skips leading blank lines to find the headline', () => {
    expect(summariseFailure('\n\n  Application deployment failed.  \nmore')!.headline).toBe(
      'Application deployment failed.',
    );
  });

  it('counts the raw lines for the disclosure label', () => {
    expect(summariseFailure(REAL.notSynced)!.lineCount).toBe(4);
    expect(summariseFailure('single line')!.lineCount).toBe(1);
    // A trailing newline yields a final empty element, which the label counts.
    expect(summariseFailure('a\nb\n')!.lineCount).toBe(3);
  });

  it('truncates a runaway headline at the cap, with an ellipsis', () => {
    const long = 'x'.repeat(400);
    const summary = summariseFailure(long)!;

    expect(summary.headline).toHaveLength(200);
    expect(summary.headline.endsWith('…')).toBe(true);
    expect(summary.raw).toBe(long);
  });

  it('leaves a headline exactly at the cap untouched', () => {
    const exact = 'x'.repeat(200);
    expect(summariseFailure(exact)!.headline).toBe(exact);
    expect(summariseFailure(exact)!.headline).not.toContain('…');
  });

  it('reports a cancellation reason verbatim', () => {
    const raw = 'Cancelled by lee.park@example.com';
    expect(summariseFailure(raw)!.headline).toBe(raw);
  });
});

describe('summariseFailure detail line', () => {
  it('carries the remainder, never repeating the headline', () => {
    const summary = summariseFailure(REAL.notAvailable)!;

    expect(summary.detail).not.toContain('Rollout status is not available');
    expect(summary.detail).toContain('List of current images');
    expect(summary.detail).toContain('ErrImagePull');
  });

  it('collapses the remainder onto one line', () => {
    expect(summariseFailure(REAL.notSynced)!.detail).toBe(
      'Unhealthy resources: Pod(app-xyz) Degraded with message Back-off pulling image "app:v0.0.2": ErrImagePull',
    );
  });

  it('has no detail for a single-line reason', () => {
    expect(summariseFailure(REAL.staleAbort)!.detail).toBeUndefined();
    expect(summariseFailure('Cancelled by alice')!.detail).toBeUndefined();
  });

  it('has no detail when the body is only blank lines', () => {
    expect(summariseFailure('headline\n\n   \n')!.detail).toBeUndefined();
  });
});
