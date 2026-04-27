import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { EmptyState, EmptyStateCta } from './EmptyState';

describe('EmptyState', () => {
  it('renders title and description', () => {
    render(<EmptyState title="Nothing here" description="Kick something off" />);
    expect(screen.getByText('Nothing here')).toBeInTheDocument();
    expect(screen.getByText('Kick something off')).toBeInTheDocument();
  });

  it('omits description when not provided', () => {
    render(<EmptyState title="Empty" />);
    expect(screen.getByText('Empty')).toBeInTheDocument();
  });

  it('renders the supplied CTA', () => {
    const onClick = vi.fn();
    render(
      <EmptyState
        title="Empty"
        cta={<EmptyStateCta label="Clear filters" onClick={onClick} />}
      />,
    );
    const button = screen.getByRole('button', { name: 'Clear filters' });
    button.click();
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
