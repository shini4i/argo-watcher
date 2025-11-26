import { useMemo, useState } from 'react';
import { Button, FormControlLabel, Menu, MenuItem, Stack, Switch } from '@mui/material';
import FileDownloadIcon from '@mui/icons-material/FileDownload';
import { useListContext, useNotify } from 'react-admin';
import type { Task } from '../../../data/types';
import type { HistoryExportFormat } from '../exportService';
import { requestHistoryExport } from '../exportService';

/** Generates a timestamped filename prefix for exported history datasets. */
const timestampFilename = () => {
  const now = new Date();
  const timestamp = now.toISOString().slice(0, 19).replace('T', '-').replaceAll(':', '-');
  return `history-tasks-${timestamp}`;
};

/** Creates a temporary link to download a blob with the provided filename. */
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
 * Export menu for history tasks allowing CSV/JSON downloads with optional anonymisation.
 */
interface HistoryExportMenuProps {
  anonymizeForced: boolean;
  disabled?: boolean;
}

/** Dropdown export menu that offers CSV/JSON downloads for the history list. */
export const HistoryExportMenu = ({ anonymizeForced, disabled = false }: HistoryExportMenuProps) => {
  const notify = useNotify();
  const { filterValues = {} } = useListContext<Task>();

  const [anchorEl, setAnchorEl] = useState<null | HTMLElement>(null);
  const [anonymize, setAnonymize] = useState<boolean>(true);
  const [exporting, setExporting] = useState(false);

  const open = Boolean(anchorEl);
  const handleOpen = (event: React.MouseEvent<HTMLButtonElement>) => {
    if (disabled || exporting) {
      return;
    }
    setAnchorEl(event.currentTarget);
  };
  const handleClose = () => setAnchorEl(null);

  const resolvedFilters = useMemo(() => {
    const start = typeof filterValues.start === 'number' ? filterValues.start : undefined;
    const end = typeof filterValues.end === 'number' ? filterValues.end : undefined;
    const app = typeof filterValues.app === 'string' ? filterValues.app : undefined;
    return { start, end, app };
  }, [filterValues]);

  const runExport = async (format: HistoryExportFormat) => {
    setExporting(true);
    try {
      const blob = await requestHistoryExport({
        format,
        anonymize: anonymizeForced ? true : anonymize,
        filters: resolvedFilters,
      });
      const filename = `${timestampFilename()}.${format}`;
      triggerDownload(blob, filename);
      notify('Export completed', { type: 'info' });
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Unexpected export failure';
      notify(message, { type: 'warning' });
    } finally {
      setExporting(false);
      handleClose();
    }
  };

  return (
    <Stack
      direction="row"
      spacing={1}
      alignItems="center"
      justifyContent="flex-end"
      sx={{ width: { xs: '100%', md: 'auto' } }}
    >
      <Button
        variant="contained"
        startIcon={<FileDownloadIcon />}
        onClick={handleOpen}
        disabled={disabled || exporting}
      >
        Export
      </Button>
      <FormControlLabel
        control={
          <Switch
            size="small"
            checked={anonymizeForced ? true : anonymize}
            onChange={event => setAnonymize(event.target.checked)}
            disabled={anonymizeForced || disabled}
          />
        }
        label="Anonymize"
      />
      <Menu anchorEl={anchorEl} open={open} onClose={handleClose}>
        <MenuItem onClick={() => runExport('json')}>Download JSON</MenuItem>
        <MenuItem onClick={() => runExport('csv')}>Download CSV</MenuItem>
      </Menu>
    </Stack>
  );
};
