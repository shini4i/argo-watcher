import { Box, Skeleton, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { tokens } from '../../../theme/tokens';
import { formatDuration } from '../../../shared/utils/time';
import type { OverviewKpis } from '../deriveOverview';
import { WINDOW_LABELS, type OverviewWindow } from '../types';

interface KpiStripProps {
  readonly kpis: OverviewKpis;
  readonly window: OverviewWindow;
  readonly isPending: boolean;
}

/** Height is fixed so the skeleton and the loaded card do not shift the page. */
const CARD_HEIGHT = 84;

const Card = ({
  eyebrow,
  value,
  sub,
  bg,
  fg,
}: {
  eyebrow: string;
  value: string;
  sub: string;
  bg: string;
  fg: string;
}) => (
  <Box
    sx={{
      backgroundColor: bg,
      borderRadius: `${tokens.radiusMd}px`,
      padding: 1.75,
      height: CARD_HEIGHT,
      minWidth: 0,
    }}
  >
    <Typography sx={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.6px', color: fg }}>
      {eyebrow}
    </Typography>
    <Typography sx={{ fontSize: 26, fontWeight: 600, lineHeight: 1.2 }}>{value}</Typography>
    <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>{sub}</Typography>
  </Box>
);

/**
 * @description The window's headline numbers. They are exact sums of the
 * backend's per-app counts, not a sample, so they need no caveat.
 */
export const KpiStrip = ({ kpis, window, isPending }: KpiStripProps) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const label = WINDOW_LABELS[window];

  if (isPending) {
    return (
      <Box sx={{ display: 'grid', gap: 2, gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr', md: 'repeat(4, 1fr)' } }}>
        {['running', 'failed', 'deployed', 'median'].map(key => (
          <Skeleton key={key} variant="rounded" height={CARD_HEIGHT} />
        ))}
      </Box>
    );
  }

  return (
    <Box
      sx={{
        display: 'grid',
        gap: 2,
        gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr', md: 'repeat(4, 1fr)' },
      }}
    >
      <Card
        eyebrow="RUNNING NOW"
        value={String(kpis.runningNow)}
        sub="deployments in flight"
        bg={isDark ? tokens.statusRunningBgDark : tokens.statusRunningBg}
        fg={isDark ? tokens.statusRunningFgDark : tokens.statusRunningFg}
      />
      <Card
        eyebrow="FAILED"
        value={String(kpis.failed)}
        sub={`in the last ${label}`}
        bg={isDark ? tokens.statusFailedBgDark : tokens.statusFailedBg}
        fg={isDark ? tokens.statusFailedFgDark : tokens.statusFailedFg}
      />
      <Card
        eyebrow="DEPLOYED"
        value={String(kpis.deployed)}
        sub={`in the last ${label}`}
        bg={isDark ? tokens.statusDeployedBgDark : tokens.statusDeployedBg}
        fg={isDark ? tokens.statusDeployedFgDark : tokens.statusDeployedFg}
      />
      <Card
        eyebrow="MEDIAN DURATION"
        value={kpis.medianDurationSeconds > 0 ? formatDuration(kpis.medianDurationSeconds) : '—'}
        sub="across applications"
        bg={isDark ? tokens.accentSoftDark : tokens.accentSoft}
        fg={tokens.accent}
      />
    </Box>
  );
};
