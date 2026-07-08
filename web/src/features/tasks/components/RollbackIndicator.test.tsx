import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { RollbackIndicator } from './RollbackIndicator';

describe('RollbackIndicator', () => {
  it('renders a labeled marker when the task is a rollback', () => {
    render(<RollbackIndicator isRollback />);
    expect(screen.getByRole('img', { name: 'Rollback' })).toBeInTheDocument();
  });

  it('renders nothing for a regular deployment', () => {
    const { container } = render(<RollbackIndicator isRollback={false} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing when the flag is undefined', () => {
    const { container } = render(<RollbackIndicator />);
    expect(container).toBeEmptyDOMElement();
  });
});
