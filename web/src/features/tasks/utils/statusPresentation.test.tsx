import { describe, expect, it } from 'vitest';
import { describeTaskStatus, type TaskStatusPresentation } from './statusPresentation';

type StatusExpectation = Pick<
  TaskStatusPresentation,
  'label' | 'chipColor' | 'timelineDotColor' | 'reasonSeverity'
>;

interface StatusCase {
  readonly status: string | null | undefined;
  readonly expected: StatusExpectation;
}

const statusCases: StatusCase[] = [
  {
    status: null,
    expected: { label: 'Unknown', chipColor: 'default', timelineDotColor: 'default', reasonSeverity: 'info' },
  },
  {
    status: 'deployed',
    expected: { label: 'Deployed', chipColor: 'success', timelineDotColor: 'success', reasonSeverity: 'success' },
  },
  {
    status: 'failed',
    expected: { label: 'Failed', chipColor: 'error', timelineDotColor: 'error', reasonSeverity: 'error' },
  },
  {
    status: 'in progress',
    expected: {
      label: 'In Progress',
      chipColor: 'warning',
      timelineDotColor: 'warning',
      reasonSeverity: 'warning',
    },
  },
  {
    status: 'app not found',
    expected: {
      label: 'App Not Found',
      chipColor: 'default',
      timelineDotColor: 'info',
      reasonSeverity: 'info',
    },
  },
  {
    status: 'custom',
    expected: { label: 'custom', chipColor: 'default', timelineDotColor: 'default', reasonSeverity: 'info' },
  },
];

describe('describeTaskStatus', () => {
  for (const { status, expected } of statusCases) {
    it(`maps status ${String(status)} to presentation metadata`, () => {
      const presentation = describeTaskStatus(status ?? undefined);
      expect(presentation.label).toBe(expected.label);
      expect(presentation.chipColor).toBe(expected.chipColor);
      expect(presentation.timelineDotColor).toBe(expected.timelineDotColor);
      expect(presentation.reasonSeverity).toBe(expected.reasonSeverity);
      expect(presentation.icon).toBeTruthy();
    });
  }
});
