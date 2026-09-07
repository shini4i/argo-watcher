import { Box, Link, Stack, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import type { ReactNode } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import type { Image, Task, TaskStatus } from '../../../../data/types';
import { tokens } from '../../../../theme/tokens';
import { describeProject } from '../../components/AppCell';
import { formatRelativeTime } from '../../../../shared/utils/time';
import { CopyChip } from './CopyChip';

/** Cap on rendered image rows, so a task with hundreds does not stall the page. */
export const MAX_IMAGES_RENDERED = 20;

const Card = ({ title, children }: { title: string; children: ReactNode }) => {
  const theme = useTheme();
  return (
    <Box
      sx={{
        border: `1px solid ${theme.palette.divider}`,
        borderRadius: `${tokens.radiusMd}px`,
        padding: 2,
        minWidth: 0,
      }}
    >
      <Typography
        sx={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.8px', color: 'text.secondary', mb: 1.5 }}
      >
        {title}
      </Typography>
      {children}
    </Box>
  );
};

const Field = ({ label, children }: { label: string; children: ReactNode }) => (
  <Stack direction="row" spacing={1} sx={{ alignItems: 'baseline', minWidth: 0 }}>
    <Typography sx={{ fontSize: 12, color: 'text.secondary', width: 110, flexShrink: 0 }}>
      {label}
    </Typography>
    <Box sx={{ fontSize: 13, minWidth: 0, overflowWrap: 'anywhere' }}>{children}</Box>
  </Stack>
);

const ProjectValue = ({ project }: { project?: string | null }) => {
  if (!project) {
    return <>—</>;
  }
  const info = describeProject(project);
  if (info.isUrl && info.href) {
    return (
      <Link href={info.href} target="_blank" rel="noopener noreferrer" underline="hover">
        {info.label}
      </Link>
    );
  }
  return <>{info.label}</>;
};

interface DeploymentCardProps {
  readonly task: TaskStatus;
  readonly previousDeploy: Task | null;
}

/** @description Who deployed what, and what this deployment replaced. */
export const TaskDeploymentCard = ({ task, previousDeploy }: DeploymentCardProps) => (
  <Card title="DEPLOYMENT">
    <Stack spacing={1}>
      <Field label="Application">{task.app ?? '—'}</Field>
      <Field label="Project">
        <ProjectValue project={task.project} />
      </Field>
      <Field label="Author">
        <Box component="span" sx={{ fontFamily: tokens.fontMono, fontSize: 12 }}>
          {task.author || '—'}
        </Box>
      </Field>
      {task.is_rollback && (
        <Field label="Rollback of">
          {task.rollback_target_id ? (
            <Link component={RouterLink} to={`/task/${task.rollback_target_id}`} underline="hover">
              {task.rollback_target_id.slice(0, 8)}
            </Link>
          ) : (
            'A previously deployed version'
          )}
        </Field>
      )}
      <Field label="Previous deploy">
        {previousDeploy ? (
          <Link component={RouterLink} to={`/task/${previousDeploy.id}`} underline="hover">
            {previousDeploy.id.slice(0, 8)} · {formatRelativeTime(previousDeploy.created)}
          </Link>
        ) : (
          'None found'
        )}
      </Field>
    </Stack>
  </Card>
);

const TagBadge = ({ tag }: { tag: string }) => (
  <Box
    component="span"
    title={tag}
    sx={{
      display: 'inline-block',
      flexShrink: 0,
      height: 18,
      lineHeight: '18px',
      padding: '0 6px',
      borderRadius: tokens.radiusPill,
      backgroundColor: theme => (theme.palette.mode === 'dark' ? tokens.accentSoftDark : tokens.accentSoft),
      color: tokens.accent,
      fontFamily: tokens.fontMono,
      fontSize: 11,
      fontWeight: 500,
    }}
  >
    {tag}
  </Box>
);

const summariseImages = (images: readonly Image[]): string => {
  if (images.length === 1) {
    return '1 image';
  }
  const tags = new Set(images.map(image => image.tag));
  return `${images.length} images${tags.size === 1 ? ' · same tag' : ''}`;
};

/** @description Every image the task deployed, each copyable as `image:tag`. */
export const TaskImagesCard = ({ images }: { images?: readonly Image[] }) => {
  const list = images ?? [];
  const rendered = list.slice(0, MAX_IMAGES_RENDERED);

  return (
    <Card title="IMAGES">
      {list.length === 0 ? (
        <Typography sx={{ fontSize: 13, color: 'text.secondary' }}>
          No container images were reported for this task.
        </Typography>
      ) : (
        <Stack spacing={1}>
          <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>
            {summariseImages(list)}
          </Typography>
          {rendered.map((image, index) => (
            <Stack
              key={`${image.image}:${image.tag}:${index}`}
              direction="row"
              spacing={1}
              sx={{ alignItems: 'center', minWidth: 0 }}
            >
              <Typography
                sx={{
                  fontFamily: tokens.fontMono,
                  fontSize: 12,
                  minWidth: 0,
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
                title={image.image}
              >
                {image.image}
              </Typography>
              <TagBadge tag={image.tag} />
              <Box sx={{ flexGrow: 1 }} />
              <CopyChip
                value={`${image.image}:${image.tag}`}
                label="Image"
                display=""
              />
            </Stack>
          ))}
          {list.length > rendered.length && (
            <Typography sx={{ fontSize: 11, color: 'text.secondary' }}>
              Showing the first {MAX_IMAGES_RENDERED} images.
            </Typography>
          )}
        </Stack>
      )}
    </Card>
  );
};
