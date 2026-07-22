import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

vi.mock('./ArgocdStatusProvider', () => ({
  useArgocdStatus: vi.fn(),
}));

import { useArgocdStatus } from './ArgocdStatusProvider';
import { ArgocdUnreachableBanner } from './ArgocdUnreachableBanner';

const mockStatus = (status: { available: boolean; reason: string | null }) => {
  (useArgocdStatus as unknown as vi.Mock).mockReturnValue(status);
};

describe('ArgocdUnreachableBanner', () => {
  it('names ArgoCD when only ArgoCD is unreachable', () => {
    mockStatus({ available: false, reason: 'argocd' });
    render(<ArgocdUnreachableBanner />);
    const statusOutput = screen.getByRole('status');
    expect(statusOutput.tagName).toBe('OUTPUT');
    expect(statusOutput).toHaveAttribute('aria-live', 'assertive');
    expect(statusOutput.textContent).toContain('cannot reach ArgoCD —');
  });

  it('names the state backend when only the database is unreachable', () => {
    mockStatus({ available: false, reason: 'database' });
    render(<ArgocdUnreachableBanner />);
    expect(screen.getByRole('status').textContent).toContain('state backend (database)');
  });

  it('names both when both are unreachable', () => {
    mockStatus({ available: false, reason: 'both' });
    render(<ArgocdUnreachableBanner />);
    expect(screen.getByRole('status').textContent).toContain('ArgoCD or its state backend');
  });

  it('falls back to naming both when the cause is unknown', () => {
    mockStatus({ available: false, reason: null });
    render(<ArgocdUnreachableBanner />);
    expect(screen.getByRole('status').textContent).toContain('ArgoCD or its state backend');
  });

  it('renders nothing when everything is reachable', () => {
    mockStatus({ available: true, reason: null });
    const { container } = render(<ArgocdUnreachableBanner />);
    expect(container).toBeEmptyDOMElement();
  });
});
