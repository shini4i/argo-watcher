import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('./useDeployLockState', () => ({
  useDeployLockState: vi.fn(),
}));

vi.mock('../argocdStatus/useArgocdUnreachable', () => ({
  useArgocdUnreachable: vi.fn(),
}));

import { useDeployLockState } from './useDeployLockState';
import { useArgocdUnreachable } from '../argocdStatus/useArgocdUnreachable';
import { DeployLockBanner } from './DeployLockBanner';

describe('DeployLockBanner', () => {
  beforeEach(() => {
    (useArgocdUnreachable as unknown as vi.Mock).mockReturnValue(false);
  });

  it('renders banner when lock is active', () => {
    (useDeployLockState as unknown as vi.Mock).mockReturnValue(true);
    render(<DeployLockBanner />);
    const statusOutput = screen.getByRole('status');
    expect(statusOutput.tagName).toBe('OUTPUT');
    expect(statusOutput).toHaveAttribute('aria-live', 'polite');
  });

  it('renders nothing when lock is inactive', () => {
    (useDeployLockState as unknown as vi.Mock).mockReturnValue(false);
    const { container } = render(<DeployLockBanner />);
    expect(container).toBeEmptyDOMElement();
  });

  it('yields to the ArgoCD-unreachable banner when ArgoCD is down', () => {
    (useDeployLockState as unknown as vi.Mock).mockReturnValue(true);
    (useArgocdUnreachable as unknown as vi.Mock).mockReturnValue(true);
    const { container } = render(<DeployLockBanner />);
    expect(container).toBeEmptyDOMElement();
  });
});
