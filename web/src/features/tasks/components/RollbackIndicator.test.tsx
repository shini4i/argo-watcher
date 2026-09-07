import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { RollbackIndicator } from './RollbackIndicator';

describe('RollbackIndicator', () => {
  it('renders a labelled chip when the task is a rollback', () => {
    render(<RollbackIndicator isRollback />);
    expect(screen.getByText('Rollback')).toBeInTheDocument();
  });

  it('keeps the chip on one line so it cannot wrap beside the app name', () => {
    render(<RollbackIndicator isRollback />);
    expect(screen.getByText('Rollback')).toHaveStyle({
      whiteSpace: 'nowrap',
      flexShrink: '0',
    });
  });

  it('explains the flag on hover', async () => {
    const user = userEvent.setup();
    render(<RollbackIndicator isRollback />);

    await user.hover(screen.getByText('Rollback'));
    expect(await screen.findByRole('tooltip')).toHaveTextContent(
      'Rollback to a previously deployed version',
    );
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
