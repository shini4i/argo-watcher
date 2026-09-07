import { Box, Stack, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { tokens } from '../../../theme/tokens';

interface ShortcutHintProps {
  /** Whether to advertise the Mine/Everyone shortcut, which is hidden anonymously. */
  readonly showScope: boolean;
}

const Key = ({ children }: { children: string }) => {
  const theme = useTheme();
  return (
    <Box
      component="kbd"
      sx={{
        fontFamily: tokens.fontMono,
        fontSize: 11,
        padding: '1px 5px',
        borderRadius: '4px',
        backgroundColor: theme.palette.mode === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(0,0,0,0.05)',
      }}
    >
      {children}
    </Box>
  );
};

/** @description Advertises the list's keyboard shortcuts in the pagination row. */
export const ShortcutHint = ({ showScope }: ShortcutHintProps) => (
  <Stack
    direction="row"
    spacing={1}
    sx={{ alignItems: 'center', flexWrap: 'wrap', color: 'text.secondary', fontSize: 12.5 }}
  >
    <Typography component="span" sx={{ fontSize: 12.5 }}>
      <Key>/</Key> search
    </Typography>
    <Typography component="span" sx={{ fontSize: 12.5 }}>
      <Key>a</Key> all
    </Typography>
    <Typography component="span" sx={{ fontSize: 12.5 }}>
      <Key>i</Key> in progress
    </Typography>
    <Typography component="span" sx={{ fontSize: 12.5 }}>
      <Key>f</Key> failed
    </Typography>
    {showScope && (
      <Typography component="span" sx={{ fontSize: 12.5 }}>
        <Key>m</Key> mine
      </Typography>
    )}
  </Stack>
);
