import { describe, expect, it } from 'vitest';
import { describeTaskStatus } from './statusPresentation';

describe('describeTaskStatus', () => {
  const cases: Array<[string | null | undefined, string, string]> = [
    [null, 'Unknown', 'default'],
    ['deployed', 'Deployed', 'success'],
    ['failed', 'Failed', 'error'],
    ['in progress', 'In Progress', 'warning'],
    ['app not found', 'App Not Found', 'default'],
    ['custom', 'custom', 'default'],
  ];

  for (const [status, label, chipColor] of cases) {
    it(`maps status ${String(status)} to label ${label}`, () => {
      const presentation = describeTaskStatus(status as string);
      expect(presentation.label).toBe(label);
      expect(presentation.chipColor).toBe(chipColor);
      expect(presentation.icon).toBeTruthy();
    });
  }

  it('defaults to fallback presentation when status missing', () => {
    const presentation = describeTaskStatus(undefined);
    expect(presentation.label).toBe('Unknown');
    expect(presentation.reasonSeverity).toBe('info');
  });
});
