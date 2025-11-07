import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

vi.mock('./useDeployLockState', () => ({
  useDeployLockState: vi.fn(),
}));

import { useDeployLockState } from './useDeployLockState';
import { DeployLockBanner } from './DeployLockBanner';

describe('DeployLockBanner', () => {
  it('renders banner when lock is active', () => {
    (useDeployLockState as unknown as vi.Mock).mockReturnValue(true);
    render(<DeployLockBanner />);
    const alert = screen.getByRole('status');
    expect(alert).toHaveAttribute('role', 'status');
    expect(alert).toHaveAttribute('aria-live', 'polite');
  });

  it('renders nothing when lock is inactive', () => {
    (useDeployLockState as unknown as vi.Mock).mockReturnValue(false);
    const { container } = render(<DeployLockBanner />);
    expect(container).toBeEmptyDOMElement();
  });
});
