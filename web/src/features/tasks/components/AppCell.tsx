import { Box, Link, Stack, Typography } from '@mui/material';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { tokens } from '../../../theme/tokens';

interface AppCellProps {
  readonly app: string;
  readonly project?: string | null;
}

const SWATCHES: ReadonlyArray<{ readonly bg: string; readonly fg: string }> = [
  { bg: '#EEF2FF', fg: '#5B7CFA' }, // blue
  { bg: '#FFF4E5', fg: '#ED6C02' }, // amber
  { bg: '#E8F5E9', fg: '#2E7D32' }, // green
  { bg: '#FBEAFF', fg: '#7B1FA2' }, // purple
  { bg: '#FDECEA', fg: '#D32F2F' }, // red
];

/** Stable hash → swatch index for a given app name. */
const hashIndex = (name: string, modulo: number): number => {
  let hash = 0;
  for (let i = 0; i < name.length; i += 1) {
    hash = (hash * 31 + name.charCodeAt(i)) | 0;
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

/** Splits a project string into either a plain label or a host + last-segment + href triple. */
export const describeProject = (project: string): ProjectLinkInfo => {
  const isUrl = project.startsWith('http://') || project.startsWith('https://');
  if (!isUrl) {
    return { isUrl: false, label: project };
  }
  const stripped = project.replace(/^https?:\/\//, '').replace(/\/+$/, '');
  const parts = stripped.split('/').filter(Boolean);
  const host = parts[0] ?? stripped;
  const lastPath = parts.length > 1 ? parts[parts.length - 1] : '';
  const label = lastPath ? `${host}/${lastPath}` : host;
  return { isUrl: true, label, href: project };
};

/**
 * Renders an application cell with a colour-coded monogram, the app name,
 * and a secondary project line that becomes an external link when the
 * project is a URL.
 */
export const AppCell = ({ app, project }: AppCellProps) => {
  const monogram = deriveMonogram(app);
  const swatch = SWATCHES[hashIndex(app, SWATCHES.length)];
  const projectInfo = project ? describeProject(project) : null;

  return (
    <Stack direction="row" spacing={1} alignItems="center" sx={{ minWidth: 0 }}>
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
      <Stack spacing={0.25} sx={{ minWidth: 0 }}>
        <Typography
          variant="body2"
          sx={{ fontWeight: 500, fontSize: 13.5, lineHeight: 1.2 }}
          noWrap
          title={app}
        >
          {app}
        </Typography>
        {projectInfo &&
          (projectInfo.isUrl && projectInfo.href ? (
            <Link
              href={projectInfo.href}
              target="_blank"
              rel="noopener noreferrer"
              underline="hover"
              onClick={event => event.stopPropagation()}
              sx={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 0.25,
                fontFamily: tokens.fontMono,
                fontSize: 11,
                color: 'text.secondary',
                maxWidth: '100%',
              }}
            >
              <Box component="span" sx={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {projectInfo.label}
              </Box>
              <OpenInNewIcon sx={{ fontSize: 11 }} />
            </Link>
          ) : (
            <Typography
              variant="caption"
              sx={{
                fontFamily: tokens.fontMono,
                fontSize: 11,
                color: 'text.secondary',
                lineHeight: 1.2,
              }}
              noWrap
              title={projectInfo.label}
            >
              {projectInfo.label}
            </Typography>
          ))}
      </Stack>
    </Stack>
  );
};
