import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { TaskFailureRow } from './TaskFailureRow';
import { summariseFailure } from '../utils/failureReason';

const notify = vi.fn();
vi.mock('react-admin', () => ({ useNotify: () => notify }));

const renderRow = (reason: string, tone?: 'error' | 'neutral') =>
  render(
    <MemoryRouter>
      <table>
        <tbody>
          <TaskFailureRow taskId="task-9" summary={summariseFailure(reason)!} colSpan={6} tone={tone} />
        </tbody>
      </table>
    </MemoryRouter>,
  );

describe('TaskFailureRow', () => {
  beforeEach(() => {
    notify.mockReset();
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
      configurable: true,
    });
  });

  it('shows the headline the backend composed', () => {
    renderRow('Application deployment failed. Rollout status is not available\n\nSync operation phase: Failed');

    expect(
      screen.getByText('Application deployment failed. Rollout status is not available'),
    ).toBeInTheDocument();
  });

  it('keeps the raw reason on screen beside the headline', () => {
    const raw =
      'Application deployment failed. Rollout status is not available\n\nSync operation message: one or more objects failed to apply, reason: Error: UPGRADE FAILED';
    renderRow(raw);

    // getByText collapses the newline, so match the raw line by its tail.
    expect(screen.getByText(/reason: Error: UPGRADE FAILED/)).toBeInTheDocument();
  });

  it('does not print a single-line reason twice', () => {
    renderRow('App is not available. Pod api-7c9: ErrImagePull');

    expect(screen.getAllByText('App is not available. Pod api-7c9: ErrImagePull')).toHaveLength(1);
  });

  it('copies the raw reason, not the headline', () => {
    const raw = 'Application deployment failed. Rollout status is not available\n\ntrailing detail';
    renderRow(raw);

    fireEvent.click(screen.getByRole('button', { name: 'Copy' }));
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(raw);
  });

  it('links through to the task without letting the click reach the row', () => {
    const onRowClick = vi.fn();
    render(
      <MemoryRouter>
        <table>
          <tbody onClick={onRowClick}>
            <TaskFailureRow taskId="task-9" summary={summariseFailure('boom')!} colSpan={6} />
          </tbody>
        </table>
      </MemoryRouter>,
    );

    const link = screen.getByRole('link', { name: 'Full reason' });
    expect(link).toHaveAttribute('href', '/task/task-9');
    fireEvent.click(link);
    expect(onRowClick).not.toHaveBeenCalled();
  });

  it('spans every data column so the panel reads as one block', () => {
    renderRow('boom');
    expect(screen.getByRole('cell')).toHaveAttribute('colspan', '6');
  });
});
