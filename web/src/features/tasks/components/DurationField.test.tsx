import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Task } from '../../../data/types';
import { DurationField } from './DurationField';

// getBrowserWindow is redirected so the ticker-lifecycle cases can observe
// setInterval/clearInterval directly. It defaults to the real window, which the
// fake timers already patch, so the text-based cases below behave normally.
let browserWindow: Window | { setInterval: unknown; clearInterval: unknown } | undefined;

vi.mock('../../../shared/utils', async importOriginal => ({
  ...(await importOriginal<typeof import('../../../shared/utils')>()),
  getBrowserWindow: () => browserWindow,
}));

const baseRecord: Task = {
  id: 'task-1',
  app: 'api',
  author: 'alice',
  created: 100,
  updated: 0,
  project: '',
  images: [],
  status: 'in progress',
};

describe('DurationField', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2025-01-01T00:00:00Z'));
    browserWindow = globalThis.window;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders compact duration for completed tasks', () => {
    render(<DurationField record={{ ...baseRecord, status: 'deployed', updated: 165 }} />);
    expect(screen.getByText('1m 05s')).toBeInTheDocument();
  });

  it('ticks every second while the task is in progress', async () => {
    const startUnix = Math.floor(Date.parse('2025-01-01T00:00:00Z') / 1000);
    const record: Task = { ...baseRecord, created: startUnix, updated: 0, status: 'in progress' };

    render(<DurationField record={record} />);
    expect(screen.getByText('0s')).toBeInTheDocument();

    await act(async () => {
      vi.advanceTimersByTime(3000);
    });
    expect(screen.getByText('3s')).toBeInTheDocument();
  });

  it('ticks while in progress even though the backend stamps updated on creation', async () => {
    const startUnix = Math.floor(Date.parse('2025-01-01T00:00:00Z') / 1000);
    const record: Task = {
      ...baseRecord,
      created: startUnix,
      updated: startUnix,
      status: 'in progress',
    };

    render(<DurationField record={record} />);
    expect(screen.getByText('0s')).toBeInTheDocument();

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });
    expect(screen.getByText('5s')).toBeInTheDocument();
  });

  it('ignores updated for in-progress tasks and keeps counting from created', async () => {
    const startUnix = Math.floor(Date.parse('2025-01-01T00:00:00Z') / 1000);
    const record: Task = {
      ...baseRecord,
      created: startUnix - 120,
      updated: startUnix - 60,
      status: 'in progress',
    };

    render(<DurationField record={record} />);
    expect(screen.getByText('2m 00s')).toBeInTheDocument();

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.getByText('2m 01s')).toBeInTheDocument();
  });

  it('renders a placeholder when a finished task has no updated timestamp', () => {
    render(<DurationField record={{ ...baseRecord, status: 'deployed', updated: 0 }} />);
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('keeps the duration stable over time for completed tasks', async () => {
    render(<DurationField record={{ ...baseRecord, status: 'deployed', updated: 130 }} />);
    expect(screen.getByText('30s')).toBeInTheDocument();

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });
    expect(screen.getByText('30s')).toBeInTheDocument();
  });

  it('freezes the duration once the task reaches a final status', async () => {
    const startUnix = Math.floor(Date.parse('2025-01-01T00:00:00Z') / 1000);
    const record: Task = {
      ...baseRecord,
      created: startUnix,
      updated: startUnix,
      status: 'in progress',
    };

    const { rerender } = render(<DurationField record={record} />);
    await act(async () => {
      vi.advanceTimersByTime(3000);
    });
    expect(screen.getByText('3s')).toBeInTheDocument();

    rerender(
      <DurationField record={{ ...record, status: 'deployed', updated: startUnix + 3 }} />,
    );
    expect(screen.getByText('3s')).toBeInTheDocument();

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });
    expect(screen.getByText('3s')).toBeInTheDocument();
  });

  it('clamps to 0s when the browser clock is behind the server', async () => {
    const startUnix = Math.floor(Date.parse('2025-01-01T00:00:00Z') / 1000);
    const record: Task = {
      ...baseRecord,
      created: startUnix + 30,
      updated: startUnix + 30,
      status: 'in progress',
    };

    render(<DurationField record={record} />);
    expect(screen.getByText('0s')).toBeInTheDocument();

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.getByText('0s')).toBeInTheDocument();
  });

  describe('ticker lifecycle', () => {
    const setIntervalMock = vi.fn();
    const clearIntervalMock = vi.fn();

    beforeEach(() => {
      setIntervalMock.mockReset().mockReturnValue(7);
      clearIntervalMock.mockReset();
      browserWindow = { setInterval: setIntervalMock, clearInterval: clearIntervalMock };
    });

    it('registers a one-second interval for an in-progress task and clears it on unmount', () => {
      const { unmount } = render(
        <DurationField record={{ ...baseRecord, status: 'in progress' }} />,
      );
      expect(setIntervalMock).toHaveBeenCalledWith(expect.any(Function), 1000);

      unmount();
      expect(clearIntervalMock).toHaveBeenCalledWith(7);
    });

    it('never registers an interval for a task in a final status', () => {
      render(<DurationField record={{ ...baseRecord, status: 'deployed', updated: 130 }} />);
      expect(setIntervalMock).not.toHaveBeenCalled();
    });
  });
});
