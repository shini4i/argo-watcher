import { relativeHumanDuration } from './Utils';
import { describe, expect, it } from '@jest/globals';

describe('relativeHumanDuration', () => {
  it('should return "< 1 minute" when seconds are less than 60', () => {
    expect(relativeHumanDuration(59)).toBe('< 1 minute');
  });

  it('should return number of minutes when seconds are between 60 and 3600', () => {
    expect(relativeHumanDuration(600)).toBe('10 minutes');
  });

  it('should return number of hours when seconds are between 3600 and 86400', () => {
    expect(relativeHumanDuration(7200)).toBe('2 hours');
  });

  it('should return number of days when seconds are between 86400 and 2620800', () => {
    expect(relativeHumanDuration(172800)).toBe('2 days');
  });

  it('should return number of months when seconds are between 2620800 and 31449600', () => {
    expect(relativeHumanDuration(5241600)).toBe('2 months');
  });

  it('should return number of years when seconds are more than 31449600', () => {
    expect(relativeHumanDuration(62899200)).toBe('2 years');
  });
});
