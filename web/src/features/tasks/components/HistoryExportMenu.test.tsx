import { fireEvent, render, screen } from '@testing-library/react';
import { ListContextProvider } from 'react-admin';
import type { ListContextValue } from 'react-admin';
import { beforeEach, describe, expect, it, vi } from 'vitest';
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

const prepareExportRowsMock = vi.fn<[Task[], boolean], unknown[]>(() => [{ id: 'row' }]);
const exportAsJsonMock = vi.fn();
const exportAsCsvMock = vi.fn();
const exportAsXlsxMock = vi.fn();

vi.mock('../exportUtils', () => ({
  prepareExportRows: (records: Task[], anonymize: boolean) => prepareExportRowsMock(records, anonymize),
  exportAsJson: (rows: unknown[], filename: string) => exportAsJsonMock(rows, filename),
  exportAsCsv: (rows: unknown[], filename: string) => exportAsCsvMock(rows, filename),
  exportAsXlsx: (rows: unknown[], filename: string) => exportAsXlsxMock(rows, filename),
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

const renderMenu = (records: Task[] = sampleRecords, anonymizeForced = false, disabled = false) => {
  const contextValue = {
    data: records,
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
    prepareExportRowsMock.mockClear();
    exportAsJsonMock.mockClear();
    exportAsCsvMock.mockClear();
    exportAsXlsxMock.mockClear();
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

    expect(prepareExportRowsMock).toHaveBeenCalledWith(sampleRecords, false);
    expect(exportAsCsvMock).toHaveBeenCalledWith(
      [{ id: 'row' }],
      expect.stringMatching(/^history-tasks-/),
    );
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

    expect(prepareExportRowsMock).toHaveBeenCalledWith(sampleRecords, true);
    expect(exportAsJsonMock).toHaveBeenCalled();
  });

  it('surfaces a warning when export preparation throws', async () => {
    prepareExportRowsMock.mockImplementationOnce(() => {
      throw new Error('failed to build export');
    });

    renderMenu();

    fireEvent.click(screen.getByRole('button', { name: /export/i }));
    const xlsxOption = await screen.findByRole('menuitem', { name: /download xlsx/i });
    fireEvent.click(xlsxOption);

    expect(notifyMock).toHaveBeenCalledWith('failed to build export', { type: 'warning' });
    expect(exportAsXlsxMock).not.toHaveBeenCalled();
  });
});
