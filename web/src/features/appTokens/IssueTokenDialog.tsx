import { useCallback, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  Stack,
  Switch,
  TextField,
} from '@mui/material';
import type { IssueAppTokenRequest } from './appTokensService';

interface IssueTokenDialogProps {
  open: boolean;
  onClose: () => void;
  onIssue: (request: IssueAppTokenRequest) => Promise<void>;
}

/** Splits the textarea into application names, dropping blank entries. */
export const parseApps = (value: string): string[] =>
  value
    .split(/[\n,]/)
    .map(app => app.trim())
    .filter(app => app.length > 0);

/**
 * Collects the scope of a new token. All applications is a deliberate second
 * choice rather than the default, since it is the credential that authorizes the
 * whole estate.
 */
export const IssueTokenDialog = ({ open, onClose, onIssue }: IssueTokenDialogProps) => {
  const [appsInput, setAppsInput] = useState('');
  const [allApps, setAllApps] = useState(false);
  const [description, setDescription] = useState('');
  const [expiresInDays, setExpiresInDays] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const apps = useMemo(() => parseApps(appsInput), [appsInput]);
  const expiryDays = Number(expiresInDays);
  const expiryValid = expiresInDays === '' || (Number.isInteger(expiryDays) && expiryDays >= 0 && expiryDays <= 3650);
  const canSubmit = (allApps || apps.length > 0) && expiryValid && !submitting;

  const reset = useCallback(() => {
    setAppsInput('');
    setAllApps(false);
    setDescription('');
    setExpiresInDays('');
    setError(null);
  }, []);

  const handleClose = useCallback(() => {
    if (submitting) {
      return;
    }
    reset();
    onClose();
  }, [onClose, reset, submitting]);

  const handleSubmit = useCallback(async () => {
    if (!canSubmit) {
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      await onIssue({
        apps: allApps ? undefined : apps,
        all_apps: allApps || undefined,
        description: description.trim() || undefined,
        expires_in_days: expiresInDays === '' ? undefined : expiryDays,
      });
      reset();
      onClose();
    } catch (err) {
      setError(err instanceof Error && err.message ? err.message : 'Failed to issue the token.');
    } finally {
      setSubmitting(false);
    }
  }, [allApps, apps, canSubmit, description, expiresInDays, expiryDays, onClose, onIssue, reset]);

  return (
    <Dialog open={open} onClose={handleClose} fullWidth maxWidth="sm">
      <DialogTitle>Issue a deploy token</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 1 }}>
          {error && <Alert severity="error">{error}</Alert>}

          <FormControlLabel
            control={
              <Switch
                checked={allApps}
                onChange={event => setAllApps(event.target.checked)}
                slotProps={{ input: { 'aria-label': 'All applications' } }}
              />
            }
            label="All applications"
          />

          <TextField
            label="Applications"
            placeholder="one per line, or comma separated"
            value={appsInput}
            onChange={event => setAppsInput(event.target.value)}
            disabled={allApps}
            multiline
            minRows={3}
            helperText={
              allApps
                ? 'This token will authorize every application, present and future.'
                : `${apps.length} application${apps.length === 1 ? '' : 's'}`
            }
            fullWidth
          />

          <TextField
            label="Description"
            placeholder="which pipeline holds this token"
            value={description}
            onChange={event => setDescription(event.target.value)}
            slotProps={{ htmlInput: { maxLength: 255 } }}
            fullWidth
          />

          <TextField
            label="Expires in (days)"
            placeholder="leave empty for no expiry"
            value={expiresInDays}
            onChange={event => setExpiresInDays(event.target.value)}
            error={!expiryValid}
            helperText={expiryValid ? 'Empty means the token never expires.' : 'Enter a whole number of days, up to 3650.'}
            fullWidth
          />
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} size="small" disabled={submitting}>
          Cancel
        </Button>
        <Button onClick={handleSubmit} variant="contained" size="small" disabled={!canSubmit}>
          Issue
        </Button>
      </DialogActions>
    </Dialog>
  );
};
