import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Task } from '../../../data/types';
import { DurationField } from './DurationField';

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
    const record: Task = { ...baseRecord, created: startUnix, status: 'in progress' };
    record.updated = null as unknown as number;

    render(<DurationField record={record} />);
    expect(screen.getByText('0s')).toBeInTheDocument();

    await act(async () => {
      vi.advanceTimersByTime(3000);
    });
    expect(screen.getByText('3s')).toBeInTheDocument();
  });
});
