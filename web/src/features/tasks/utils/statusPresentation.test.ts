import { describe, expect, it } from 'vitest';
import { describeTaskStatus } from './statusPresentation';

describe('describeTaskStatus', () => {
  it('returns default presentation when status missing', () => {
    expect(describeTaskStatus()).toMatchObject({
      label: 'Unknown',
      chipColor: 'default',
      timelineDotColor: 'default',
      reasonSeverity: 'info',
    });
  });

  it('maps known statuses to themed presentation', () => {
    expect(describeTaskStatus('deployed')).toMatchObject({
      label: 'Deployed',
      chipColor: 'success',
      timelineDotColor: 'success',
      reasonSeverity: 'success',
    });

    expect(describeTaskStatus('failed')).toMatchObject({
      label: 'Failed',
      chipColor: 'error',
      timelineDotColor: 'error',
      reasonSeverity: 'error',
    });
  });

  it('falls back to neutral display for unknown status text', () => {
    expect(describeTaskStatus('mystery')).toMatchObject({
      label: 'mystery',
      chipColor: 'default',
      timelineDotColor: 'default',
      reasonSeverity: 'info',
    });
  });
});
