import Papa from 'papaparse';
import * as XLSX from 'xlsx';
import type { Task } from '../../data/types';

/**
 * Normalized shape for export rows. All values are serialisable primitives.
 */
export type ExportRow = Record<string, string | number | boolean | null>;

/** Flattens a Task into a serialisable row, optionally removing author/reason. */
const sanitizeTask = (task: Task, anonymize: boolean): ExportRow => {
  const base: ExportRow = {
    id: task.id,
    app: task.app,
    project: task.project,
    status: task.status ?? '',
    created: task.created,
    updated: task.updated,
    images: task.images.map(image => `${image.image}:${image.tag}`).join(', '),
  };

  if (!anonymize) {
    base.author = task.author;
    base.status_reason = task.status_reason ?? '';
  }

  return base;
};

/**
 * Converts task records into export rows applying optional anonymisation.
 */
export const prepareExportRows = (records: Task[], anonymize: boolean): ExportRow[] =>
  records.map(record => sanitizeTask(record, anonymize));

/** Creates a temporary link to download the given blob with the provided filename. */
const triggerDownload = (blob: Blob, filename: string) => {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
};

/**
 * Generates a JSON export from the provided rows.
 */
export const exportAsJson = (rows: ExportRow[], filename: string) => {
  const blob = new Blob([JSON.stringify(rows, null, 2)], { type: 'application/json' });
  triggerDownload(blob, `${filename}.json`);
};

/**
 * Generates a CSV export from the provided rows.
 */
export const exportAsCsv = (rows: ExportRow[], filename: string) => {
  const csv = Papa.unparse(rows);
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
  triggerDownload(blob, `${filename}.csv`);
};

/**
 * Generates an XLSX export from the provided rows.
 */
export const exportAsXlsx = (rows: ExportRow[], filename: string) => {
  const worksheet = XLSX.utils.json_to_sheet(rows);
  const workbook = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(workbook, worksheet, 'History');
  const buffer = XLSX.write(workbook, { bookType: 'xlsx', type: 'array' });
  const blob = new Blob([buffer], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' });
  triggerDownload(blob, `${filename}.xlsx`);
};
