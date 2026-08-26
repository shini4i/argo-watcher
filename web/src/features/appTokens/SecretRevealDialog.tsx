import { useCallback, useState } from 'react';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import {
  Alert,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Stack,
  TextField,
  Tooltip,
} from '@mui/material';

interface SecretRevealDialogProps {
  secret: string | null;
  onClose: () => void;
}

/**
 * Shows a freshly issued token once. Nothing persists the secret, so closing this
 * dialog is the point past which it cannot be recovered — the copy affordance and
 * the warning are the whole reason it exists.
 */
export const SecretRevealDialog = ({ secret, onClose }: SecretRevealDialogProps) => {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(async () => {
    if (!secret) {
      return;
    }

    try {
      await navigator.clipboard.writeText(secret);
      setCopied(true);
    } catch {
      // A denied clipboard permission is not an error worth interrupting for: the
      // secret is on screen and selectable.
      setCopied(false);
    }
  }, [secret]);

  const handleClose = useCallback(() => {
    setCopied(false);
    onClose();
  }, [onClose]);

  return (
    <Dialog open={Boolean(secret)} onClose={handleClose} fullWidth maxWidth="sm">
      <DialogTitle>Copy your deploy token</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 1 }}>
          <Alert severity="warning">
            This is the only time the token is shown. Store it in your pipeline&apos;s secret
            manager now — if you lose it, revoke it and issue a new one.
          </Alert>

          <Stack direction="row" spacing={1} alignItems="flex-start">
            <TextField
              label="Token"
              value={secret ?? ''}
              slotProps={{ htmlInput: { readOnly: true, 'aria-label': 'Deploy token secret' } }}
              multiline
              fullWidth
            />
            <Tooltip title={copied ? 'Copied' : 'Copy to clipboard'}>
              <IconButton onClick={handleCopy} aria-label="Copy deploy token">
                <ContentCopyIcon />
              </IconButton>
            </Tooltip>
          </Stack>
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} variant="contained" size="small">
          Done
        </Button>
      </DialogActions>
    </Dialog>
  );
};
