import { Box, Link, Stack, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { tokens } from '../../../theme/tokens';
import { RollbackIndicator } from './RollbackIndicator';

interface AppCellProps {
  readonly app: string;
  readonly project?: string | null;
  readonly isRollback?: boolean;
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

// Both text lines are nowrap, so without an explicit cap they set the cell's
// min-content width under `table-layout: auto` and stretch the table sideways.
export const APP_TEXT_MAX_WIDTH = 230;

const SUBTITLE_SX = {
  display: 'block',
  maxWidth: APP_TEXT_MAX_WIDTH,
  fontFamily: tokens.fontMono,
  fontSize: 11,
  color: 'text.secondary',
  lineHeight: 1.2,
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

/** Absent rather than an em-dash: the row already names the app on the line above. */
const ProjectSubtitle = ({ project }: { project?: string | null }) => {
  if (!project) {
    return null;
  }

  const info = describeProject(project);
  if (info.isUrl && info.href) {
    return (
      <Link
        href={info.href}
        target="_blank"
        rel="noopener noreferrer"
        underline="hover"
        onClick={event => event.stopPropagation()}
        title={info.href}
        sx={{ ...SUBTITLE_SX, display: 'inline-flex', alignItems: 'center', gap: 0.25 }}
      >
        <Box
          component="span"
          sx={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
        >
          {info.label}
        </Box>
        <OpenInNewIcon aria-hidden sx={{ fontSize: 11, flexShrink: 0 }} />
      </Link>
    );
  }

  return (
    <Typography component="span" title={info.label} sx={SUBTITLE_SX}>
      {info.label}
    </Typography>
  );
};

/**
 * @description Application identity for a task row: monogram, app name, the
 * rollback flag, and the project as a subtitle. Carries the project so the list
 * needs no separate column for it.
 */
export const AppCell = ({ app, project, isRollback }: AppCellProps) => {
  const theme = useTheme();
  const swatches = theme.palette.mode === 'dark' ? tokens.monogramSwatchesDark : tokens.monogramSwatches;
  const monogram = deriveMonogram(app);
  const swatch = swatches[hashIndex(app, swatches.length)];

  return (
    <Stack direction="row" spacing={1} sx={{ alignItems: 'center', minWidth: 0 }}>
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
      <Stack spacing={0.25} sx={{ minWidth: 0, maxWidth: APP_TEXT_MAX_WIDTH }}>
        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center', minWidth: 0 }}>
          <Typography
            variant="body2"
            sx={{
              display: 'block',
              maxWidth: APP_TEXT_MAX_WIDTH,
              fontWeight: 500,
              fontSize: 13.5,
              lineHeight: 1.2,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
            title={app}
          >
            {app}
          </Typography>
          <RollbackIndicator isRollback={isRollback} />
        </Stack>
        <ProjectSubtitle project={project} />
      </Stack>
    </Stack>
  );
};
