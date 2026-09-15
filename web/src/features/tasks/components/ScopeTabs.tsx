import { Box } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { tokens } from '../../../theme/tokens';
import { PillTabs, type PillTabSpec } from './PillTabs';

export type TaskScope = 'mine' | 'everyone';

interface ScopeTabsProps {
  readonly value: TaskScope;
  readonly onChange: (next: TaskScope) => void;
  /** Signed-in address, used for the initials badge. */
  readonly identityEmail: string;
}

/** First letters of the local part, e.g. jane.doe@acme → JD. */
export const deriveInitials = (email: string): string => {
  const local = email.split('@')[0] ?? '';
  const parts = local.split(/[._-]+/).filter(Boolean);
  if (parts.length === 0) {
    return '?';
  }
  if (parts.length === 1) {
    return parts[0].slice(0, 2).toUpperCase();
  }
  return `${parts[0][0]}${parts[1][0]}`.toUpperCase();
};

/**
 * @description Scopes the list to the signed-in user's own deployments. It
 * drives the backend's `author` filter, so it composes with the search box
 * rather than competing for it.
 */
export const ScopeTabs = ({ value, onChange, identityEmail }: ScopeTabsProps) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';

  const tabs: PillTabSpec[] = [
    {
      id: 'mine',
      label: 'Mine',
      adornment: (
        <Box
          aria-hidden
          component="span"
          sx={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 16,
            height: 16,
            borderRadius: '50%',
            backgroundColor: isDark ? tokens.accentSoftDark : tokens.accentSoft,
            color: tokens.accent,
            fontSize: 8.5,
            fontWeight: 700,
          }}
        >
          {deriveInitials(identityEmail)}
        </Box>
      ),
    },
    { id: 'everyone', label: 'Everyone' },
  ];

  return (
    <PillTabs
      tabs={tabs}
      value={value}
      onChange={next => onChange(next === 'mine' ? 'mine' : 'everyone')}
      ariaLabel="Deployment scope"
    />
  );
};
