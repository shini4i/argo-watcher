import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

vi.mock('./useArgocdUnreachable', () => ({
  useArgocdUnreachable: vi.fn(),
}));

import { useArgocdUnreachable } from './useArgocdUnreachable';
import { ArgocdUnreachableBanner } from './ArgocdUnreachableBanner';

describe('ArgocdUnreachableBanner', () => {
  it('renders banner when ArgoCD is unreachable', () => {
    (useArgocdUnreachable as unknown as vi.Mock).mockReturnValue(true);
    render(<ArgocdUnreachableBanner />);
    // <output> has an implicit role of "status"; aria-live escalates urgency.
    const statusOutput = screen.getByRole('status');
    expect(statusOutput.tagName).toBe('OUTPUT');
    expect(statusOutput).toHaveAttribute('aria-live', 'assertive');
  });

  it('renders nothing when ArgoCD is reachable', () => {
    (useArgocdUnreachable as unknown as vi.Mock).mockReturnValue(false);
    const { container } = render(<ArgocdUnreachableBanner />);
    expect(container).toBeEmptyDOMElement();
  });
});
