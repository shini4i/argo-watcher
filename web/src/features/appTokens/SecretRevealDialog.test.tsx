import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { SecretRevealDialog } from './SecretRevealDialog';

vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);

/**
 * Copying is how an operator captures a secret that is displayed once and cannot be
 * recovered. A silent regression here loses the credential and leaves a live token
 * in Postgres that nobody holds.
 */
describe('SecretRevealDialog', () => {
  const writeText = vi.fn();
  const onClose = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    writeText.mockResolvedValue(undefined);
  });

  // userEvent.setup() installs its own navigator.clipboard, so the stub has to be
  // put in place afterwards or it is silently replaced.
  const setupWithClipboard = () => {
    const user = userEvent.setup();
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
      writable: true,
    });
    return user;
  };

  it('renders nothing without a secret', () => {
    render(<SecretRevealDialog secret={null} onClose={onClose} />);

    expect(screen.queryByLabelText('Deploy token secret')).not.toBeInTheDocument();
  });

  it('copies the secret verbatim and confirms it', async () => {
    const user = setupWithClipboard();
    render(<SecretRevealDialog secret="awt_theOneAndOnlyCopy" onClose={onClose} />);

    const button = screen.getByRole('button', { name: 'Copy deploy token' });
    await user.click(button);

    expect(writeText).toHaveBeenCalledWith('awt_theOneAndOnlyCopy');

    // The confirmation lives in the tooltip, which MUI only mounts on hover.
    await user.hover(button);
    expect(await screen.findByRole('tooltip')).toHaveTextContent('Copied');
  });

  it('shows the secret read-only so it cannot be edited before copying', () => {
    render(<SecretRevealDialog secret="awt_readOnly" onClose={onClose} />);

    const field = screen.getByLabelText('Deploy token secret');
    expect(field).toHaveValue('awt_readOnly');
    expect(field).toHaveAttribute('readonly');
  });

  it('survives a denied clipboard permission without losing the dialog', async () => {
    const user = setupWithClipboard();
    writeText.mockRejectedValue(new Error('permission denied'));
    render(<SecretRevealDialog secret="awt_stillVisible" onClose={onClose} />);

    const button = screen.getByRole('button', { name: 'Copy deploy token' });
    await user.click(button);

    // The secret stays on screen and selectable; nothing claims it was copied.
    await waitFor(() => {
      expect(screen.getByLabelText('Deploy token secret')).toHaveValue('awt_stillVisible');
    });
    await user.hover(button);
    expect(await screen.findByRole('tooltip')).toHaveTextContent('Copy to clipboard');
  });

  it('closes on Done', async () => {
    const user = setupWithClipboard();
    render(<SecretRevealDialog secret="awt_done" onClose={onClose} />);

    await user.click(screen.getByRole('button', { name: 'Done' }));

    expect(onClose).toHaveBeenCalled();
  });

  it('does not carry a stale confirmation to the next token', async () => {
    const user = setupWithClipboard();
    // One instance throughout, as AppTokensPage drives it: the dialog is rendered
    // unconditionally and only MUI's Dialog unmounts its children, so the copied
    // flag lives across close and reopen. handleClose's reset is the only thing
    // stopping the next token from opening on a "Copied" it never earned.
    const { rerender } = render(<SecretRevealDialog secret="awt_first" onClose={onClose} />);

    await user.click(screen.getByRole('button', { name: 'Copy deploy token' }));
    await waitFor(() => expect(writeText).toHaveBeenCalled());

    await user.click(screen.getByRole('button', { name: 'Done' }));
    rerender(<SecretRevealDialog secret={null} onClose={onClose} />);
    rerender(<SecretRevealDialog secret="awt_second" onClose={onClose} />);

    await user.hover(screen.getByRole('button', { name: 'Copy deploy token' }));
    expect(await screen.findByRole('tooltip')).toHaveTextContent('Copy to clipboard');
  });
});
