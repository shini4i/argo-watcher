import type { ReactNode } from 'react';
import { act, renderHook } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

(
  globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;

vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);

vi.mock('./argocdStatusService', () => ({
  argocdStatusService: {
    subscribe: vi.fn(),
  },
}));

import { ArgocdStatusProvider } from './ArgocdStatusProvider';
import { useArgocdUnreachable } from './useArgocdUnreachable';

let subscribeMock: vi.Mock;

beforeAll(async () => {
  const module = await import('./argocdStatusService');
  subscribeMock = module.argocdStatusService.subscribe as vi.Mock;
});

describe('useArgocdUnreachable', () => {
  it('reflects reachability updates from the service subscription', () => {
    let listener: ((available: boolean) => void) | null = null;
    subscribeMock.mockImplementation(cb => {
      listener = cb;
      return () => undefined;
    });

    const wrapper = ({ children }: { children: ReactNode }) => (
      <ArgocdStatusProvider>{children}</ArgocdStatusProvider>
    );

    const { result } = renderHook(() => useArgocdUnreachable(), { wrapper });
    // Available by default -> not unreachable.
    expect(result.current).toBe(false);

    act(() => listener?.(false));
    expect(result.current).toBe(true);

    act(() => listener?.(true));
    expect(result.current).toBe(false);
  });
});
