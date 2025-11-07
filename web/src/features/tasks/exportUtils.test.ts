import { describe, expect, it } from 'vitest';
import type { Task } from '../../data/types';
import { prepareExportRows } from './exportUtils';

const sampleTask: Task = {
  id: '1',
  app: 'demo',
  project: 'proj',
  author: 'alice',
  status: 'deployed',
  created: 100,
  updated: 200,
  images: [{ image: 'img', tag: 'latest' }],
  status_reason: 'Success',
};

describe('exportUtils', () => {
  it('retains author fields when anonymisation disabled', () => {
    const rows = prepareExportRows([sampleTask], false);
    expect(rows[0]).toMatchObject({ author: 'alice', status_reason: 'Success' });
  });

  it('removes sensitive fields when anonymisation enabled', () => {
    const rows = prepareExportRows([sampleTask], true);
    expect(rows[0]).not.toHaveProperty('author');
    expect(rows[0]).not.toHaveProperty('status_reason');
  });
});
