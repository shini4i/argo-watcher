import { render, screen, act } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { NotificationPayload } from 'ra-core';
import { AppNotification } from './AppNotification';

const notificationsQueue: NotificationPayload[] = [];
const takeNotification = vi.fn();
const translateMock = vi.fn(message => message);
const takeUndoableMutationMock = vi.fn();
const notifyContextMock = {
  notifications: notificationsQueue,
  takeNotification: () => {
    const notification = notificationsQueue.shift();
    takeNotification(notification);
    return notification;
  },
};

vi.mock('ra-core', async () => {
  const actual = await vi.importActual<typeof import('ra-core')>('ra-core');
  return {
    ...actual,
    useNotificationContext: () => ({
      notifications: [...notificationsQueue],
      takeNotification: notifyContextMock.takeNotification,
    }),
    useTakeUndoableMutation: () => takeUndoableMutationMock,
    useTranslate: () => translateMock,
  };
});

describe('AppNotification', () => {
  beforeEach(() => {
    notificationsQueue.length = 0;
    takeNotification.mockReset();
    translateMock.mockImplementation(message => message);
    takeUndoableMutationMock.mockReset();
  });

  it('renders the latest notification as an alert', async () => {
    notificationsQueue.push({
      message: 'hello',
      type: 'success',
      notificationOptions: {},
    });

    render(<AppNotification />);

    expect(await screen.findByText('hello')).toBeInTheDocument();
  });

  it('keeps network error messages visible with helper text', async () => {
    notificationsQueue.push({
      message: 'Network error',
      type: 'error',
      notificationOptions: {},
    });

    render(<AppNotification autoHideDuration={2000} />);

    expect(await screen.findByText(/Unable to reach the Argo Watcher API/i)).toBeInTheDocument();
  });

  it('triggers undo callback when undoable notification dismissed with undo', async () => {
    const mutationMock = vi.fn();
    takeUndoableMutationMock.mockReturnValue(mutationMock);

    notificationsQueue.push({
      message: 'undo',
      type: 'warning',
      notificationOptions: {
        undoable: true,
      },
    });

    render(<AppNotification />);

    const button = await screen.findByRole('button', { name: /undo/i });
    await act(async () => {
      button.click();
    });

    expect(mutationMock).toHaveBeenCalledWith({ isUndo: true });
  });
});
