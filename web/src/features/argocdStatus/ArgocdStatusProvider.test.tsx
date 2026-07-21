import { act, renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ArgocdStatusProvider, useArgocdStatus } from './ArgocdStatusProvider';
import { argocdStatusService } from './argocdStatusService';

vi.mock('./argocdStatusService', () => ({
  argocdStatusService: {
    subscribe: vi.fn(),
  },
}));

describe('ArgocdStatusProvider', () => {
  it('subscribes to the status service and updates state', async () => {
    const listeners: Array<(state: boolean) => void> = [];
    vi.mocked(argocdStatusService.subscribe).mockImplementation(listener => {
      listeners.push(listener);
      return () => {
        const index = listeners.indexOf(listener);
        if (index >= 0) {
          listeners.splice(index, 1);
        }
      };
    });

    const { result, unmount } = renderHook(() => useArgocdStatus(), {
      wrapper: ArgocdStatusProvider,
    });

    // Optimistic default before the first update arrives.
    expect(result.current.available).toBe(true);

    await act(async () => {
      for (const callback of listeners) {
        callback(false);
      }
    });
    await waitFor(() => expect(result.current.available).toBe(false));

    unmount();
    expect(listeners).toHaveLength(0);
  });

  it('throws when hook is used outside provider', () => {
    const wrapper = () => renderHook(() => useArgocdStatus());
    expect(wrapper).toThrow('useArgocdStatus must be used within an ArgocdStatusProvider');
  });
});
