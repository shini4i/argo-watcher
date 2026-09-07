import { Box, Skeleton, Stack, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Link as RouterLink } from 'react-router-dom';
import { tokens } from '../../../theme/tokens';
import { formatDuration, formatRelativeTime } from '../../../shared/utils/time';
import { deriveMonogram } from '../../tasks/components/AppCell';
import { describeTaskStatus } from '../../tasks/utils/statusPresentation';
import { summariseFailure } from '../../tasks/utils/failureReason';
import { deriveAppState, type AppState } from '../deriveOverview';
import { RecentOutcomeStrip } from './RecentOutcomeStrip';
import type { AppSummary } from '../types';

interface AttentionCardsProps {
  readonly summaries: readonly AppSummary[];
  readonly isPending: boolean;
}

const CARD_HEIGHT = 152;

const CHIP_TEXT: Readonly<Record<AppState, string>> = {
  failing: 'Failing',
  running: 'Deploying',
  deployed: 'Deployed',
  idle: 'Idle',
};

const hashIndex = (name: string, modulo: number): number => {
  let hash = 0;
  for (let i = 0; i < name.length; i += 1) {
    hash = Math.imul(hash, 31) + (name.codePointAt(i) ?? 0);
  }
  return Math.abs(hash) % modulo;
};

/** The last-event line names the failure when there is one; that is the point. */
const lastEventText = (summary: AppSummary): string => {
  const failure = summariseFailure(summary.last_status_reason);
  if (failure) {
    return failure.headline;
  }
  const descriptor = describeTaskStatus(summary.last_status);
  return `${descriptor.displayLabel} ${formatRelativeTime(summary.last_created)}`;
};

const AttentionCard = ({ summary }: { summary: AppSummary }) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const state = deriveAppState(summary);
  const descriptor = describeTaskStatus(summary.last_status);
  const chipFg = isDark ? descriptor.pillFgDark : descriptor.pillFg;
  const chipBg = isDark ? descriptor.pillBgDark : descriptor.pillBg;
  const swatches = isDark ? tokens.monogramSwatchesDark : tokens.monogramSwatches;
  const swatch = swatches[hashIndex(summary.app, swatches.length)];
  const failureRate = summary.total > 0 ? Math.round((summary.failed / summary.total) * 100) : 0;

  return (
    <Box
      component={RouterLink}
      to={`/tasks?app=${encodeURIComponent(summary.app)}`}
      sx={{
        display: 'block',
        textDecoration: 'none',
        color: 'inherit',
        border: `1px solid ${theme.palette.divider}`,
        borderLeft: `4px solid ${chipFg}`,
        borderRadius: `${tokens.radiusMd}px`,
        backgroundColor: isDark ? tokens.surfaceDark : tokens.surface,
        padding: 1.75,
        height: CARD_HEIGHT,
        minWidth: 0,
        transition: 'background-color 150ms ease',
        '&:hover': {
          backgroundColor: isDark ? tokens.rowHoverDark : tokens.rowHoverLight,
        },
      }}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', minWidth: 0 }}>
        <Box
          aria-hidden
          sx={{
            width: 26,
            height: 26,
            flexShrink: 0,
            borderRadius: `${tokens.radiusSm}px`,
            backgroundColor: swatch.bg,
            color: swatch.fg,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontSize: 11,
            fontWeight: 600,
          }}
        >
          {deriveMonogram(summary.app)}
        </Box>
        <Box sx={{ minWidth: 0, flexGrow: 1 }}>
          <Typography noWrap sx={{ fontSize: 13, fontWeight: 600 }} title={summary.app}>
            {summary.app}
          </Typography>
          <Typography
            noWrap
            sx={{ fontFamily: tokens.fontMono, fontSize: 10.5, color: 'text.secondary' }}
            title={summary.project}
          >
            {summary.project}
          </Typography>
        </Box>
        <Box
          component="span"
          sx={{
            flexShrink: 0,
            height: 20,
            lineHeight: '20px',
            padding: '0 8px',
            borderRadius: tokens.radiusPill,
            backgroundColor: chipBg,
            color: chipFg,
            fontSize: 11,
            fontWeight: 600,
            whiteSpace: 'nowrap',
          }}
        >
          {CHIP_TEXT[state]}
        </Box>
      </Stack>

      <Box sx={{ mt: 1.5 }}>
        <RecentOutcomeStrip statuses={summary.recent_statuses} />
      </Box>

      <Typography sx={{ fontSize: 11.5, color: 'text.secondary', mt: 1 }}>
        {summary.failed}/{summary.total} failed ({failureRate}%)
        {summary.median_duration_seconds > 0
          ? ` · median ${formatDuration(summary.median_duration_seconds)}`
          : ''}
      </Typography>
      <Typography
        noWrap
        sx={{ fontSize: 11.5, color: 'text.secondary' }}
        title={summary.last_status_reason || undefined}
      >
        {lastEventText(summary)}
      </Typography>
    </Box>
  );
};

/**
 * @description Cards for the applications that failed or are deploying in the
 * window, ranked failing first. A clean application is left to the list below —
 * a card per app would bury what needs attention.
 */
export const AttentionCards = ({ summaries, isPending }: AttentionCardsProps) => {
  if (isPending) {
    return (
      <Box sx={{ display: 'grid', gap: 2, gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' } }}>
        <Skeleton variant="rounded" height={CARD_HEIGHT} />
        <Skeleton variant="rounded" height={CARD_HEIGHT} />
      </Box>
    );
  }

  if (summaries.length === 0) {
    return (
      <Typography sx={{ fontSize: 13, color: 'text.secondary' }}>
        Nothing failing or deploying in this window.
      </Typography>
    );
  }

  return (
    <Box
      sx={{
        display: 'grid',
        gap: 2,
        gridTemplateColumns: { xs: '1fr', md: '1fr 1fr', lg: 'repeat(3, 1fr)' },
      }}
    >
      {summaries.map(summary => (
        <AttentionCard key={summary.app} summary={summary} />
      ))}
    </Box>
  );
};
