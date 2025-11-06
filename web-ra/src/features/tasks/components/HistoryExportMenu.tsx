import { useMemo, useState } from 'react';
import { Button, FormControlLabel, Menu, MenuItem, Stack, Switch } from '@mui/material';
import FileDownloadIcon from '@mui/icons-material/FileDownload';
import { useListContext, useNotify } from 'react-admin';
import type { Task } from '../../../data/types';
import { exportAsCsv, exportAsJson, exportAsXlsx, prepareExportRows } from '../exportUtils';

const timestampFilename = () => {
  const now = new Date();
  return `history-tasks-${now.toISOString().replace(/[:T]/g, '-').slice(0, 19)}`;
};

/**
 * Export menu for history tasks allowing CSV/JSON/XLSX downloads with optional anonymisation.
 */
export const HistoryExportMenu = ({ anonymizeForced }: { anonymizeForced: boolean }) => {
  const notify = useNotify();
  const { data } = useListContext<Task>();
  const records = useMemo(() => (Array.isArray(data) ? data : []), [data]);

  const [anchorEl, setAnchorEl] = useState<null | HTMLElement>(null);
  const [anonymize, setAnonymize] = useState<boolean>(true);

  const open = Boolean(anchorEl);
  const handleOpen = (event: React.MouseEvent<HTMLButtonElement>) => setAnchorEl(event.currentTarget);
  const handleClose = () => setAnchorEl(null);

  const runExport = (format: 'json' | 'csv' | 'xlsx') => {
    try {
      const rows = prepareExportRows(records, anonymizeForced ? true : anonymize);
      const filename = timestampFilename();

      switch (format) {
        case 'json':
          exportAsJson(rows, filename);
          break;
        case 'csv':
          exportAsCsv(rows, filename);
          break;
        case 'xlsx':
          exportAsXlsx(rows, filename);
          break;
      }

      notify('Export completed', { type: 'info' });
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Unexpected export failure';
      notify(message, { type: 'warning' });
    } finally {
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
      <Button variant="contained" startIcon={<FileDownloadIcon />} onClick={handleOpen} disabled={records.length === 0}>
        Export
      </Button>
      <FormControlLabel
        control={
          <Switch
            size="small"
            checked={anonymizeForced ? true : anonymize}
            onChange={event => setAnonymize(event.target.checked)}
            disabled={anonymizeForced}
          />
        }
        label="Anonymize"
      />
      <Menu anchorEl={anchorEl} open={open} onClose={handleClose}>
        <MenuItem onClick={() => runExport('json')}>Download JSON</MenuItem>
        <MenuItem onClick={() => runExport('csv')}>Download CSV</MenuItem>
        <MenuItem onClick={() => runExport('xlsx')}>Download XLSX</MenuItem>
      </Menu>
    </Stack>
  );
};
