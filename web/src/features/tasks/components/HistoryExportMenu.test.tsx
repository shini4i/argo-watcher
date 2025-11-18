import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { ListContextProvider } from 'react-admin';
import type { ListContextValue } from 'react-admin';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Task } from '../../../data/types';
import { HistoryExportMenu } from './HistoryExportMenu';

const notifyMock = vi.fn();

vi.mock('react-admin', async () => {
  const actual = await vi.importActual<typeof import('react-admin')>('react-admin');
  return {
    ...actual,
    useNotify: () => notifyMock,
  };
});

const requestHistoryExportMock = vi.fn(() => Promise.resolve(new Blob(['ok'], { type: 'text/csv' })));

let originalCreateObjectURL: ((obj: Blob | MediaSource) => string) | undefined;
let originalRevokeObjectURL: ((url: string) => void) | undefined;

vi.mock('../exportService', () => ({
  requestHistoryExport: (params: unknown) => requestHistoryExportMock(params),
}));

const sampleRecords: Task[] = [
  {
    id: '1',
    created: 1,
    updated: 2,
    app: 'alpha',
    author: 'alice',
    project: 'proj',
    images: [],
  },
];

const renderMenu = (
  records: Task[] = sampleRecords,
  anonymizeForced = false,
  disabled = false,
  filterValues: Record<string, unknown> = {},
) => {
  const contextValue = {
    data: records,
    filterValues,
  } as unknown as ListContextValue<Task>;

  return render(
    <ListContextProvider value={contextValue}>
      <HistoryExportMenu anonymizeForced={anonymizeForced} disabled={disabled} />
    </ListContextProvider>,
  );
};

describe('HistoryExportMenu', () => {
  beforeEach(() => {
    notifyMock.mockClear();
    requestHistoryExportMock.mockReset();
    requestHistoryExportMock.mockResolvedValue(new Blob(['content'], { type: 'text/csv' }));
    originalCreateObjectURL = (URL as unknown as Record<string, unknown>).createObjectURL as
      | ((obj: Blob | MediaSource) => string)
      | undefined;
    originalRevokeObjectURL = (URL as unknown as Record<string, unknown>).revokeObjectURL as
      | ((url: string) => void)
      | undefined;
    (URL as unknown as Record<string, unknown>).createObjectURL = vi.fn(() => 'blob:mock');
    (URL as unknown as Record<string, unknown>).revokeObjectURL = vi.fn();
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

  it('disables the export button when there are no records', () => {
    renderMenu([]);
    expect(screen.getByRole('button', { name: /export/i })).toBeDisabled();
  });

  it('disables export entirely when disabled prop is true', () => {
    renderMenu(sampleRecords, false, true);
    const exportButton = screen.getByRole('button', { name: /export/i });
    expect(exportButton).toBeDisabled();
  });

  it('allows toggling anonymisation and exporting CSV data', async () => {
    renderMenu();

    const anonymizeToggle = screen.getByLabelText('Anonymize');
    expect(anonymizeToggle).toBeChecked();

    fireEvent.click(anonymizeToggle);
    expect(anonymizeToggle).not.toBeChecked();

    fireEvent.click(screen.getByRole('button', { name: /export/i }));
    const csvOption = await screen.findByRole('menuitem', { name: /download csv/i });
    fireEvent.click(csvOption);

    await waitFor(() => {
      expect(requestHistoryExportMock).toHaveBeenCalledWith({
        format: 'csv',
        anonymize: false,
        filters: { start: undefined, end: undefined, app: undefined },
      });
    });
    expect(notifyMock).toHaveBeenCalledWith('Export completed', { type: 'info' });
  });

  it('forces anonymisation when anonymizeForced is true', async () => {
    renderMenu(sampleRecords, true);

    const anonymizeToggle = screen.getByLabelText('Anonymize');
    expect(anonymizeToggle).toBeDisabled();
    expect(anonymizeToggle).toBeChecked();

    fireEvent.click(screen.getByRole('button', { name: /export/i }));
    const jsonOption = await screen.findByRole('menuitem', { name: /download json/i });
    fireEvent.click(jsonOption);

    await waitFor(() => {
      expect(requestHistoryExportMock).toHaveBeenCalledWith({
        format: 'json',
        anonymize: true,
        filters: { start: undefined, end: undefined, app: undefined },
      });
    });
  });

  it('surfaces a warning when export preparation throws', async () => {
    requestHistoryExportMock.mockRejectedValueOnce(new Error('failed to build export'));

    renderMenu(sampleRecords, false, false, { start: 10, end: 20, app: 'beta' });

    fireEvent.click(screen.getByRole('button', { name: /export/i }));
    const csvOption = await screen.findByRole('menuitem', { name: /download csv/i });
    fireEvent.click(csvOption);

    await waitFor(() => {
      expect(requestHistoryExportMock).toHaveBeenCalledWith({
        format: 'csv',
        anonymize: true,
        filters: { start: 10, end: 20, app: 'beta' },
      });
    });
    expect(notifyMock).toHaveBeenCalledWith('failed to build export', { type: 'warning' });
  });
});
