import { renderHook } from '@testing-library/react';
import { act } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useCopyToClipboard } from './useCopyToClipboard';

const notify = vi.fn();
vi.mock('react-admin', () => ({ useNotify: () => notify }));

const setClipboard = (writeText: () => Promise<void>) => {
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
};

describe('useCopyToClipboard', () => {
  beforeEach(() => {
    notify.mockReset();
  });

  it('copies the text and confirms with a toast', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    setClipboard(writeText);

    const { result } = renderHook(() => useCopyToClipboard());
    await act(async () => {
      await result.current('boom', 'Reason');
    });

    expect(writeText).toHaveBeenCalledWith('boom');
    expect(notify).toHaveBeenCalledWith('Reason copied', { type: 'info' });
  });

  it('tells the user to copy manually when the clipboard is unavailable', async () => {
    setClipboard(vi.fn().mockRejectedValue(new Error('denied')));

    const { result } = renderHook(() => useCopyToClipboard());
    await act(async () => {
      await result.current('boom', 'Reason');
    });

    expect(notify).toHaveBeenCalledWith(expect.stringContaining('copy manually'), {
      type: 'warning',
    });
  });

  it('does nothing for empty text', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    setClipboard(writeText);

    const { result } = renderHook(() => useCopyToClipboard());
    await act(async () => {
      await result.current('');
    });

    expect(writeText).not.toHaveBeenCalled();
    expect(notify).not.toHaveBeenCalled();
  });
});
