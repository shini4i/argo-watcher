import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { IssueTokenDialog, parseApps } from './IssueTokenDialog';

vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);

describe('parseApps', () => {
  it.each([
    ['app1', ['app1']],
    ['app1\napp2', ['app1', 'app2']],
    ['app1, app2', ['app1', 'app2']],
    ['  app1  \n\n  app2 ', ['app1', 'app2']],
    ['', []],
    ['   \n  ', []],
  ])('parses %j', (input, expected) => {
    expect(parseApps(input)).toEqual(expected);
  });
});

describe('IssueTokenDialog', () => {
  const onIssue = vi.fn();
  const onClose = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    onIssue.mockResolvedValue(undefined);
  });

  const renderDialog = () =>
    render(<IssueTokenDialog open onClose={onClose} onIssue={onIssue} />);

  it('refuses to submit an empty scope', async () => {
    renderDialog();

    expect(screen.getByRole('button', { name: 'Issue' })).toBeDisabled();
  });

  it('submits an explicit application list', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.type(screen.getByLabelText(/^Applications/), 'app1, app2');
    await user.type(screen.getByLabelText(/^Description/), 'ci pipeline');
    await user.click(screen.getByRole('button', { name: 'Issue' }));

    await waitFor(() => {
      expect(onIssue).toHaveBeenCalledWith({
        apps: ['app1', 'app2'],
        all_apps: undefined,
        description: 'ci pipeline',
        expires_in_days: undefined,
      });
    });
  });

  it('sends the wildcard instead of a list', async () => {
    const user = userEvent.setup();
    renderDialog();

    // A stale list must not travel with the wildcard: the server rejects a request
    // that sets both.
    await user.type(screen.getByLabelText(/^Applications/), 'app1');
    await user.click(screen.getByLabelText('All applications'));
    await user.click(screen.getByRole('button', { name: 'Issue' }));

    await waitFor(() => {
      expect(onIssue).toHaveBeenCalledWith({
        apps: undefined,
        all_apps: true,
        description: undefined,
        expires_in_days: undefined,
      });
    });
  });

  it('disables the application list under the wildcard', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(screen.getByLabelText('All applications'));

    expect(screen.getByLabelText(/^Applications/)).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Issue' })).toBeEnabled();
  });

  it('passes a requested expiry through', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(screen.getByLabelText('All applications'));
    await user.type(screen.getByLabelText(/^Expires in/), '30');
    await user.click(screen.getByRole('button', { name: 'Issue' }));

    await waitFor(() => {
      expect(onIssue).toHaveBeenCalledWith(expect.objectContaining({ expires_in_days: 30 }));
    });
  });

  it.each([['4000'], ['-1'], ['1.5'], ['abc']])('blocks submission on an expiry of %s', async input => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(screen.getByLabelText('All applications'));
    await user.type(screen.getByLabelText(/^Expires in/), input);

    expect(screen.getByRole('button', { name: 'Issue' })).toBeDisabled();
    expect(onIssue).not.toHaveBeenCalled();
  });

  it('keeps the dialog open and shows why a request was refused', async () => {
    const user = userEvent.setup();
    onIssue.mockRejectedValue(new Error('a token must not list more than 200 applications'));
    renderDialog();

    await user.click(screen.getByLabelText('All applications'));
    await user.click(screen.getByRole('button', { name: 'Issue' }));

    expect(await screen.findByText(/not list more than 200 applications/)).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });

  it('clears the form when cancelled', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.type(screen.getByLabelText(/^Applications/), 'app1');
    await user.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(onClose).toHaveBeenCalled();
    expect(screen.getByLabelText(/^Applications/)).toHaveValue('');
  });
});
