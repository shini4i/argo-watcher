import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';
import Papa from 'papaparse';
import type { Task } from '../../data/types';
import { exportAsCsv, exportAsJson, exportAsXlsx, prepareExportRows } from './exportUtils';

const sampleTasks: Task[] = [
  {
    id: '1',
    app: 'demo',
    author: 'alice',
    project: 'https://github.com/org/repo',
    created: 100,
    updated: 200,
    images: [
      { image: 'api', tag: '1' },
      { image: 'worker', tag: '2' },
    ],
    status: 'ok',
    status_reason: 'all good',
  },
];

describe('exportUtils', () => {
let anchorMock: HTMLAnchorElement;
let originalCreateObjectURL: ((obj: Blob | MediaSource) => string) | undefined;
let originalRevokeObjectURL: ((url: string) => void) | undefined;

beforeEach(() => {
  anchorMock = {
    click: vi.fn(),
    remove: vi.fn(),
  } as unknown as HTMLAnchorElement;
  originalCreateObjectURL = (URL as unknown as Record<string, unknown>).createObjectURL as
    | ((obj: Blob | MediaSource) => string)
    | undefined;
  originalRevokeObjectURL = (URL as unknown as Record<string, unknown>).revokeObjectURL as
    | ((url: string) => void)
    | undefined;
  (URL as unknown as Record<string, unknown>).createObjectURL = vi.fn(() => 'blob:mock');
  (URL as unknown as Record<string, unknown>).revokeObjectURL = vi.fn();
  vi.spyOn(document, 'createElement').mockReturnValue(anchorMock);
  vi.spyOn(document.body, 'appendChild').mockImplementation(() => anchorMock);
});

afterEach(() => {
  if (originalCreateObjectURL) {
    (URL as unknown as Record<string, unknown>).createObjectURL = originalCreateObjectURL;
  } else {
    delete (URL as unknown as Record<string, unknown>).createObjectURL;
  }
  if (originalRevokeObjectURL) {
    (URL as unknown as Record<string, unknown>).revokeObjectURL = originalRevokeObjectURL;
  } else {
    delete (URL as unknown as Record<string, unknown>).revokeObjectURL;
  }
  vi.restoreAllMocks();
});

  it('prepares export rows with optional anonymization', () => {
    const rows = prepareExportRows(sampleTasks, false);
    expect(rows[0]).toMatchObject({
      author: 'alice',
      status_reason: 'all good',
      images: 'api:1, worker:2',
    });

    const anonymized = prepareExportRows(sampleTasks, true);
    expect(anonymized[0]).not.toHaveProperty('author');
    expect(anonymized[0]).not.toHaveProperty('status_reason');
  });

  it('exports rows as JSON and triggers download', () => {
    exportAsJson(sampleTasks as never, 'tasks');
    const { createObjectURL, revokeObjectURL } = URL as unknown as {
      createObjectURL: ReturnType<typeof vi.fn>;
      revokeObjectURL: ReturnType<typeof vi.fn>;
    };
    expect(anchorMock.download).toBe('tasks.json');
    expect(anchorMock.click).toHaveBeenCalled();
    expect(createObjectURL).toHaveBeenCalledWith(expect.any(Blob));
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock');
  });

  it('exports rows as CSV using Papa.unparse', () => {
    const unparseSpy = vi.spyOn(Papa, 'unparse').mockReturnValue('csv-content');
    exportAsCsv(sampleTasks as never, 'tasks');
    expect(unparseSpy).toHaveBeenCalledWith(expect.any(Array));
    expect(anchorMock.download).toBe('tasks.csv');
  });

  it('exports rows as XLSX using sheet utilities', () => {
    exportAsXlsx(sampleTasks as never, 'tasks');
    expect(anchorMock.download).toBe('tasks.xlsx');
  });
});
