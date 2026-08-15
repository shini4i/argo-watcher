import { Box, Stack, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { tokens } from '../../../theme/tokens';

interface AppCellProps {
  readonly app: string;
}

const hashIndex = (name: string, modulo: number): number => {
  let hash = 0;
  for (let i = 0; i < name.length; i += 1) {
    hash = Math.imul(hash, 31) + (name.codePointAt(i) ?? 0);
  }
  return Math.abs(hash) % modulo;
};

/** Derives 1-2 letter monogram initials from an app name (e.g. checkout-api → CA). */
export const deriveMonogram = (name: string): string => {
  const trimmed = name.trim();
  if (!trimmed) {
    return '?';
  }
  const segments = trimmed.split(/[-_/.\s]+/).filter(Boolean);
  if (segments.length === 0) {
    return trimmed.slice(0, 2).toUpperCase();
  }
  if (segments.length === 1) {
    return segments[0].slice(0, 2).toUpperCase();
  }
  return `${segments[0][0] ?? ''}${segments[1][0] ?? ''}`.toUpperCase();
};

interface ProjectLinkInfo {
  readonly isUrl: boolean;
  readonly label: string;
  readonly href?: string;
}

/**
 * For a URL, `label` is host + the final path segment only — mid-path segments
 * are dropped. Anything else passes through unchanged.
 */
export const describeProject = (project: string): ProjectLinkInfo => {
  const isUrl = project.startsWith('http://') || project.startsWith('https://');
  if (!isUrl) {
    return { isUrl: false, label: project };
  }
  let stripped = project.replace(/^https?:\/\//, '');
  while (stripped.endsWith('/')) {
    stripped = stripped.slice(0, -1);
  }
  const parts = stripped.split('/').filter(Boolean);
  const host = parts[0] ?? stripped;
  const lastPath = parts.length > 1 ? parts[parts.length - 1] : '';
  const label = lastPath ? `${host}/${lastPath}` : host;
  return { isUrl: true, label, href: project };
};

export const AppCell = ({ app }: AppCellProps) => {
  const theme = useTheme();
  const swatches = theme.palette.mode === 'dark' ? tokens.monogramSwatchesDark : tokens.monogramSwatches;
  const monogram = deriveMonogram(app);
  const swatch = swatches[hashIndex(app, swatches.length)];

  return (
    <Stack
      direction="row"
      spacing={1}
      sx={{
        alignItems: 'center',
        minWidth: 0
      }}>
      <Box
        aria-hidden
        sx={{
          width: 28,
          height: 28,
          borderRadius: `${tokens.radiusSm}px`,
          backgroundColor: swatch.bg,
          color: swatch.fg,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: 12,
          fontWeight: 600,
          flexShrink: 0,
          letterSpacing: 0.2,
        }}
      >
        {monogram}
      </Box>
      <Typography
        variant="body2"
        sx={{ fontWeight: 500, fontSize: 13.5, lineHeight: 1.2, minWidth: 0 }}
        noWrap
        title={app}
      >
        {app}
      </Typography>
    </Stack>
  );
};
