import { act, renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { DeployLockProvider, useDeployLock } from './DeployLockProvider';
import { deployLockService } from './deployLockService';

vi.mock('./deployLockService', () => ({
  deployLockService: {
    subscribe: vi.fn(),
    setLock: vi.fn(),
    releaseLock: vi.fn(),
  },
}));

describe('DeployLockProvider', () => {
  it('subscribes to deploy-lock service and updates state', async () => {
    const listeners: Array<(state: boolean) => void> = [];
    vi.mocked(deployLockService.subscribe).mockImplementation(listener => {
      listeners.push(listener);
      return () => {
        const index = listeners.indexOf(listener);
        if (index >= 0) {
          listeners.splice(index, 1);
        }
      };
    });

    const { result, unmount } = renderHook(() => useDeployLock(), {
      wrapper: DeployLockProvider,
    });

    expect(result.current.locked).toBe(false);
    await act(async () => listeners.forEach(cb => cb(true)));
    await waitFor(() => expect(result.current.locked).toBe(true));

    unmount();
    expect(listeners).toHaveLength(0);
  });

  it('proxies setLock and releaseLock actions', async () => {
    vi.mocked(deployLockService.subscribe).mockImplementation(listener => {
      listener(false);
      return () => undefined;
    });
    const { result } = renderHook(() => useDeployLock(), { wrapper: DeployLockProvider });

    await result.current.setLock();
    await result.current.releaseLock();

    expect(deployLockService.setLock).toHaveBeenCalled();
    expect(deployLockService.releaseLock).toHaveBeenCalled();
  });

  it('throws when hook is used outside provider', () => {
    const wrapper = () => renderHook(() => useDeployLock());
    expect(wrapper).toThrow('useDeployLock must be used within a DeployLockProvider');
  });
});
