import type { ReactNode } from 'react';
import { act, renderHook } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

(
  globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;

vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);

vi.mock('./deployLockService', () => ({
  deployLockService: {
    subscribe: vi.fn(),
  },
}));

import { DeployLockProvider } from './DeployLockProvider';
import { useDeployLockState } from './useDeployLockState';

let subscribeMock: vi.Mock;

beforeAll(async () => {
  const module = await import('./deployLockService');
  subscribeMock = module.deployLockService.subscribe as vi.Mock;
});

describe('useDeployLockState', () => {
  it('reflects updates from the deploy-lock service subscription', () => {
    let listener: ((locked: boolean) => void) | null = null;
    subscribeMock.mockImplementation(cb => {
      listener = cb;
      return () => undefined;
    });

    const wrapper = ({ children }: { children: ReactNode }) => (
      <DeployLockProvider>{children}</DeployLockProvider>
    );

    const { result } = renderHook(() => useDeployLockState(), { wrapper });
    expect(result.current).toBe(false);

    act(() => listener?.(true));
    expect(result.current).toBe(true);
  });
});
