import { useCallback, useEffect, useState } from 'react';
import {
  type AppToken,
  type IssueAppTokenRequest,
  issueAppToken,
  listAppTokens,
  revokeAppToken,
} from './appTokensService';

interface UseAppTokens {
  tokens: AppToken[];
  loading: boolean;
  error: string | null;
  reload: () => Promise<void>;
  issue: (request: IssueAppTokenRequest) => Promise<AppToken>;
  revoke: (id: string) => Promise<void>;
}

const messageOf = (err: unknown, fallback: string): string =>
  err instanceof Error && err.message ? err.message : fallback;

/**
 * Loads the token list and exposes the two mutations, reloading after each so the
 * table reflects what the server holds rather than an optimistic guess.
 *
 * `issue` returns the created token because its `secret` is shown once and never
 * appears in a later list.
 */
export const useAppTokens = (): UseAppTokens => {
  const [tokens, setTokens] = useState<AppToken[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      setTokens(await listAppTokens());
      setError(null);
    } catch (err) {
      setError(messageOf(err, 'Failed to load deploy tokens.'));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const issue = useCallback(
    async (request: IssueAppTokenRequest) => {
      const created = await issueAppToken(request);
      await reload();
      return created;
    },
    [reload],
  );

  const revoke = useCallback(
    async (id: string) => {
      await revokeAppToken(id);
      await reload();
    },
    [reload],
  );

  return { tokens, loading, error, reload, issue, revoke };
};
