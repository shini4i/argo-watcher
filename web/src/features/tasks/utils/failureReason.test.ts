import { describe, expect, it } from 'vitest';
import { summariseFailure } from './failureReason';

describe('summariseFailure', () => {
  it('returns null for an absent reason', () => {
    expect(summariseFailure(undefined)).toBeNull();
    expect(summariseFailure(null)).toBeNull();
    expect(summariseFailure('   ')).toBeNull();
  });

  it('lifts the message out of a Helm execution error and keeps its location', () => {
    const raw = [
      'rpc error: code = Unknown desc = Manifest generation error (cached): `helm template .',
      "--name-template payments --namespace prod` failed exit status 1: Error: execution error at",
      '(payments/templates/deployment.yaml:34:18): resources.limits.memory is required',
    ].join(' ');

    expect(summariseFailure(raw)).toMatchObject({
      headline: 'resources.limits.memory is required',
      location: 'payments/templates/deployment.yaml:34:18',
    });
  });

  it('falls back to a bare Error: segment when there is no location', () => {
    const raw =
      "Manifest generation error: `kustomize build .` failed exit status 1: Error: accumulating resources: no matches for OriginalId ~G_v1_Service|gateway";

    const summary = summariseFailure(raw)!;
    expect(summary.headline).toBe(
      'accumulating resources: no matches for OriginalId ~G_v1_Service|gateway',
    );
    expect(summary.location).toBeUndefined();
  });

  it('uses the first non-empty line verbatim when nothing can be extracted', () => {
    const raw = '\n\nApp is not available. Pod checkout-api-7c9: Back-off pulling image (ErrImagePull)\nmore detail';

    expect(summariseFailure(raw)).toMatchObject({
      headline: 'App is not available. Pod checkout-api-7c9: Back-off pulling image (ErrImagePull)',
    });
  });

  it('never drops the raw text, whichever branch produced the headline', () => {
    const raw = 'Error: execution error at (a/b.yaml:1:2): boom\nhelm.go:84: [debug] error: boom';
    expect(summariseFailure(raw)!.raw).toBe(raw);
  });

  it('counts the lines of the raw reason for the disclosure label', () => {
    expect(summariseFailure('one\ntwo\nthree')!.lineCount).toBe(3);
    expect(summariseFailure('single line')!.lineCount).toBe(1);
  });

  it('trims a runaway headline so it cannot break the row layout', () => {
    const long = `Error: ${'x'.repeat(400)}`;
    const summary = summariseFailure(long)!;
    expect(summary.headline.length).toBeLessThanOrEqual(200);
    expect(summary.headline.endsWith('…')).toBe(true);
  });

  it('stops the headline at the first newline of a multi-line Error: segment', () => {
    const raw = 'Error: first thing broke\nsecond line of detail';
    expect(summariseFailure(raw)!.headline).toBe('first thing broke');
  });

  it('collapses the single-line reason of a cancelled task without inventing structure', () => {
    const raw = 'Cancelled by lee.park@example.com';
    expect(summariseFailure(raw)).toMatchObject({ headline: raw, lineCount: 1 });
  });
});

// The headline is documented as always non-empty; a truncated reason whose
// `Error:` segment carries no message must not render a blank bold line.
describe('summariseFailure with an empty message after Error:', () => {
  it.each([
    ['Error:    ', 'Error:'],
    ['Error:', 'Error:'],
    // Falls through to the bare-error branch, which still names the template.
    ['Error: execution error at (a/b.yaml:1:2):   ', 'execution error at (a/b.yaml:1:2):'],
  ])('keeps a non-empty headline for %j', (raw, expected) => {
    const summary = summariseFailure(raw)!;
    expect(summary.headline.length).toBeGreaterThan(0);
    expect(summary.headline).toBe(expected);
    expect(summary.raw).toBe(raw);
  });

  it('reports no location when the execution message is blank', () => {
    // Falling through to the first-line branch means the location is dropped
    // with it — a location without a message would read as the whole failure.
    expect(summariseFailure('Error: execution error at (a/b.yaml:1:2):   ')!.location).toBeUndefined();
  });

  it('still lifts a real message that merely has padding', () => {
    expect(summariseFailure('Error:   boom   ')!.headline).toBe('boom');
  });
});
