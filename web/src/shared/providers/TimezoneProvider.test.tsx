import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import * as timeUtils from '../utils/time';
import { TimezoneProvider, useTimezone } from './TimezoneProvider';

const wrapper = ({ children }: { children: ReactNode }) => <TimezoneProvider>{children}</TimezoneProvider>;

describe('TimezoneProvider', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('initializes from localStorage and persists timezone changes', () => {
    window.localStorage.setItem('argo-watcher:timezone', 'local');

    const { result } = renderHook(() => useTimezone(), { wrapper });
    expect(result.current.timezone).toBe('local');

    act(() => result.current.setTimezone('utc'));
    expect(window.localStorage.getItem('argo-watcher:timezone')).toBe('utc');
  });

  it('formats dates using UTC vs local options', () => {
    const formatSpy = vi.spyOn(timeUtils, 'formatDateTime').mockReturnValue('formatted');
    const { result } = renderHook(() => useTimezone(), { wrapper });

    result.current.formatDate(123, { month: 'short' });
    expect(formatSpy).toHaveBeenCalledWith(123, undefined, expect.objectContaining({ timeZone: 'UTC' }));

    act(() => result.current.setTimezone('local'));
    result.current.formatDate(456);
    expect(formatSpy).toHaveBeenLastCalledWith(
      456,
      undefined,
      expect.not.objectContaining({ timeZone: 'UTC' }),
    );

    formatSpy.mockRestore();
  });

  it('falls back to UTC formatter when used outside provider', () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const originalDev = import.meta.env.DEV;
    (import.meta.env as Record<string, unknown>).DEV = true;

    const { result } = renderHook(() => useTimezone());
    expect(result.current.timezone).toBe('utc');

    act(() => result.current.setTimezone('local'));
    expect(warnSpy).toHaveBeenCalled();

    const output = result.current.formatDate(789);
    expect(typeof output).toBe('string');

    (import.meta.env as Record<string, unknown>).DEV = originalDev;
    warnSpy.mockRestore();
  });
});
