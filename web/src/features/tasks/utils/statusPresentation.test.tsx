import { describe, expect, it } from 'vitest';
import {
  describeTaskStatus,
  hasInformativeReason,
  isFailedStatus,
  isRunningStatus,
  type TaskStatusPresentation,
} from './statusPresentation';

type StatusExpectation = Pick<
  TaskStatusPresentation,
  'label' | 'displayLabel' | 'chipColor' | 'timelineDotColor' | 'reasonSeverity'
>;

interface StatusCase {
  readonly status: string | null | undefined;
  readonly expected: StatusExpectation;
}

const statusCases: StatusCase[] = [
  {
    status: null,
    expected: {
      label: 'Unknown',
      displayLabel: 'Unknown',
      chipColor: 'default',
      timelineDotColor: 'default',
      reasonSeverity: 'info',
    },
  },
  {
    status: 'deployed',
    expected: {
      label: 'Deployed',
      displayLabel: 'Deployed',
      chipColor: 'success',
      timelineDotColor: 'success',
      reasonSeverity: 'success',
    },
  },
  {
    status: 'failed',
    expected: {
      label: 'Failed',
      displayLabel: 'Failed',
      chipColor: 'error',
      timelineDotColor: 'error',
      reasonSeverity: 'error',
    },
  },
  {
    status: 'in progress',
    expected: {
      label: 'In Progress',
      displayLabel: 'Running',
      chipColor: 'warning',
      timelineDotColor: 'warning',
      reasonSeverity: 'warning',
    },
  },
  {
    status: 'app not found',
    expected: {
      label: 'App Not Found',
      displayLabel: 'Not found',
      chipColor: 'default',
      timelineDotColor: 'info',
      reasonSeverity: 'info',
    },
  },
  {
    status: 'cancelled',
    expected: {
      label: 'Cancelled',
      displayLabel: 'Cancelled',
      chipColor: 'default',
      timelineDotColor: 'default',
      reasonSeverity: 'info',
    },
  },
  {
    status: 'custom',
    expected: {
      label: 'custom',
      displayLabel: 'custom',
      chipColor: 'default',
      timelineDotColor: 'default',
      reasonSeverity: 'info',
    },
  },
];

describe('describeTaskStatus', () => {
  for (const { status, expected } of statusCases) {
    it(`maps status ${String(status)} to presentation metadata`, () => {
      const presentation = describeTaskStatus(status ?? undefined);
      expect(presentation.label).toBe(expected.label);
      expect(presentation.displayLabel).toBe(expected.displayLabel);
      expect(presentation.chipColor).toBe(expected.chipColor);
      expect(presentation.timelineDotColor).toBe(expected.timelineDotColor);
      expect(presentation.reasonSeverity).toBe(expected.reasonSeverity);
      expect(presentation.icon).toBeTruthy();
      expect(presentation.pillBg).toMatch(/^(rgba?\(|#)/);
      expect(presentation.pillFg).toMatch(/^(rgba?\(|#)/);
      expect(presentation.pillBgDark).toMatch(/^(rgba?\(|#)/);
      expect(presentation.pillFgDark).toMatch(/^(rgba?\(|#)/);
    });
  }
});

/**
 * @description internal/models/constants.go `failedTaskStatuses` is the source
 * of truth for this set. It is duplicated here because the browser cannot read
 * Go; pinning the members means a divergence fails a test instead of silently
 * losing a row's red edge and its place in the overview's failure count.
 */
describe('isFailedStatus / isRunningStatus', () => {
  const FAILED = [
    'failed',
    'aborted',
    'argocd is unavailable',
    'cannot connect to database',
    'failed to login to argocd',
  ];

  it.each(FAILED)('treats %j as failed', status => {
    expect(isFailedStatus(status)).toBe(true);
  });

  it('recognises exactly those five, no more', () => {
    const everyStatus = [
      ...FAILED,
      'deployed',
      'in progress',
      'cancelled',
      'app not found',
      'accepted',
    ];
    expect(everyStatus.filter(isFailedStatus)).toEqual(FAILED);
  });

  it.each(['deployed', 'in progress', 'cancelled', 'app not found', 'accepted', '', 'Failed'])(
    'does not treat %j as failed',
    status => {
      expect(isFailedStatus(status)).toBe(false);
    },
  );

  it('handles an absent status', () => {
    expect(isFailedStatus(undefined)).toBe(false);
    expect(isFailedStatus(null)).toBe(false);
    expect(isRunningStatus(undefined)).toBe(false);
  });

  it('treats only "in progress" as running', () => {
    expect(isRunningStatus('in progress')).toBe(true);
    for (const status of ['deployed', 'failed', 'accepted', 'In Progress']) {
      expect(isRunningStatus(status)).toBe(false);
    }
  });

  describe('hasInformativeReason', () => {
    it('withholds a panel from cancelled, whose reason restates the status', () => {
      expect(hasInformativeReason('cancelled')).toBe(false);
    });

    it('keeps the panel for every status whose reason carries a diagnosis', () => {
      for (const status of ['failed', 'aborted', 'app not found', 'deployed', 'in progress']) {
        expect(hasInformativeReason(status), status).toBe(true);
      }
    });

    it('keeps the panel when the status is missing — nothing says it is redundant', () => {
      expect(hasInformativeReason(undefined)).toBe(true);
      expect(hasInformativeReason(null)).toBe(true);
    });
  });
});
