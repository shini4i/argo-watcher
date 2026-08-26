import { useCallback, useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  Paper,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import { useNotify, usePermissions } from 'react-admin';
import { formatDateTime, hasPrivilegedAccess } from '../../shared/utils';
import { useAppTokensAvailable } from '../../shared/hooks/useAppTokensAvailable';
import { type AppToken, describeScope, isTokenActive } from './appTokensService';
import { useAppTokens } from './useAppTokens';
import { IssueTokenDialog } from './IssueTokenDialog';
import { SecretRevealDialog } from './SecretRevealDialog';

/** Renders a timestamp, or an em dash where the server sent none. */
const timestamp = (value?: number): string => (value ? formatDateTime(value) : '—');

const StatusChip = ({ token }: { token: AppToken }) => {
  if (token.revoked_at) {
    return <Chip label="Revoked" size="small" color="error" variant="outlined" />;
  }
  if (!isTokenActive(token)) {
    return <Chip label="Expired" size="small" color="warning" variant="outlined" />;
  }
  return <Chip label="Active" size="small" color="success" variant="outlined" />;
};

/**
 * Management page for application deploy tokens. Reachable only with privileged
 * access: the list names who holds a credential for which applications, so it is
 * as restricted as issuing one.
 */
export const AppTokensPage = () => {
  const notify = useNotify();
  const available = useAppTokensAvailable();
  const { permissions } = usePermissions();
  const { tokens, loading, error, issue, revoke } = useAppTokens();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [secret, setSecret] = useState<string | null>(null);
  const [revoking, setRevoking] = useState<string | null>(null);

  const groups: readonly string[] = (permissions as { groups?: string[] })?.groups ?? [];
  const privilegedGroups: readonly string[] =
    (permissions as { privilegedGroups?: string[] })?.privilegedGroups ?? [];
  const privileged = hasPrivilegedAccess(groups, privilegedGroups);

  const handleIssue = useCallback(
    async (request: Parameters<typeof issue>[0]) => {
      const created = await issue(request);
      setSecret(created.secret ?? null);
      notify('Deploy token issued.', { type: 'info' });
    },
    [issue, notify],
  );

  const handleRevoke = useCallback(
    async (token: AppToken) => {
      setRevoking(token.id);
      try {
        await revoke(token.id);
        notify('Deploy token revoked.', { type: 'info' });
      } catch (err) {
        notify(err instanceof Error ? err.message : 'Failed to revoke the token.', { type: 'error' });
      } finally {
        setRevoking(null);
      }
    },
    [notify, revoke],
  );

  // Default-deny while the config is still loading (available === null): the
  // endpoints do not exist without OIDC and Postgres, so there is nothing to manage.
  if (available !== true || !privileged) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="info">
          Deploy tokens require authentication, the Postgres state backend, and privileged
          access.
        </Alert>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <Stack direction="row" alignItems="center" sx={{ mb: 2, width: '100%' }}>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Typography variant="h6">Deploy tokens</Typography>
          <Typography variant="body2" color="text.secondary">
            Credentials a pipeline presents to authorize a git write-back.
          </Typography>
        </Box>
        <Button
          variant="contained"
          size="small"
          onClick={() => setDialogOpen(true)}
          sx={{ ml: 'auto', flexShrink: 0 }}
        >
          Issue token
        </Button>
      </Stack>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}

      {loading && tokens.length === 0 ? (
        <Stack alignItems="center" sx={{ py: 4 }}>
          <CircularProgress size={28} />
        </Stack>
      ) : (
        <TableContainer component={Paper} variant="outlined">
          <Table size="small" aria-label="Deploy tokens">
            <TableHead>
              <TableRow>
                <TableCell>Token</TableCell>
                <TableCell>Scope</TableCell>
                <TableCell>Description</TableCell>
                <TableCell>Issued by</TableCell>
                <TableCell>Created</TableCell>
                <TableCell>Expires</TableCell>
                <TableCell>Last used</TableCell>
                <TableCell>Status</TableCell>
                <TableCell align="right">Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {tokens.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={9}>
                    <Typography variant="body2" color="text.secondary">
                      No deploy tokens have been issued.
                    </Typography>
                  </TableCell>
                </TableRow>
              ) : (
                tokens.map(token => (
                  <TableRow key={token.id} hover>
                    <TableCell>
                      <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
                        awt_…{token.hint}
                      </Typography>
                    </TableCell>
                    <TableCell>{describeScope(token)}</TableCell>
                    <TableCell>{token.description || '—'}</TableCell>
                    <TableCell>{token.created_by}</TableCell>
                    <TableCell>{timestamp(token.created_at)}</TableCell>
                    <TableCell>{token.expires_at ? timestamp(token.expires_at) : 'Never'}</TableCell>
                    <TableCell>{timestamp(token.last_used_at)}</TableCell>
                    <TableCell>
                      <StatusChip token={token} />
                    </TableCell>
                    <TableCell align="right">
                      {token.revoked_at ? null : (
                        <Button
                          size="small"
                          color="error"
                          disabled={revoking === token.id}
                          onClick={() => void handleRevoke(token)}
                        >
                          Revoke
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      <IssueTokenDialog open={dialogOpen} onClose={() => setDialogOpen(false)} onIssue={handleIssue} />
      <SecretRevealDialog secret={secret} onClose={() => setSecret(null)} />
    </Box>
  );
};
