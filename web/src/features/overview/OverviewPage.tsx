import { useEffect, useMemo } from 'react';
import { Box, Stack, Typography } from '@mui/material';
import { useStore } from 'react-admin';
import { describeReadFailure } from '../../data/readFailure';
import { EmptyState, EmptyStateCta } from '../tasks/components/EmptyState';
import { PillTabs } from '../tasks/components/PillTabs';
import { AllApplications } from './components/AllApplications';
import { AttentionCards } from './components/AttentionCards';
import { KpiStrip } from './components/KpiStrip';
import { PinnedApps } from './components/PinnedApps';
import { deriveKpis, needsAttention, rankByAttention } from './deriveOverview';
import { useAppSummaries } from './useAppSummaries';
import { usePinnedApps } from './usePinnedApps';
import { isOverviewWindow, WINDOW_LABELS, type OverviewWindow } from './types';

const WINDOW_STORE_KEY = 'overview.window';

const WINDOW_TABS = (['24h', '7d', '30d'] as const).map(id => ({
  id,
  label: WINDOW_LABELS[id],
}));

const SectionHeading = ({ children }: { children: string }) => (
  <Typography sx={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.8px', color: 'text.secondary' }}>
    {children}
  </Typography>
);

/** Routed at `/overview`. */
export const OverviewPage = () => {
  const [storedWindow, setStoredWindow] = useStore<OverviewWindow>(WINDOW_STORE_KEY, '24h');
  const window: OverviewWindow = isOverviewWindow(storedWindow) ? storedWindow : '24h';
  const { apps, isPending, error, refetch } = useAppSummaries(window);
  const { pinned, pin, unpin, shareLink } = usePinnedApps();

  useEffect(() => {
    const previousTitle = document.title;
    document.title = 'Overview — Argo Watcher';
    return () => {
      document.title = previousTitle;
    };
  }, []);

  const kpis = useMemo(() => deriveKpis(apps), [apps]);
  const attention = useMemo(() => rankByAttention(apps.filter(needsAttention)), [apps]);
  const appNames = useMemo(
    () => apps.map(summary => summary.app).sort((left, right) => left.localeCompare(right)),
    [apps],
  );

  const windowSelector = (
    <PillTabs
      tabs={WINDOW_TABS}
      value={window}
      onChange={next => setStoredWindow(isOverviewWindow(next) ? next : '24h')}
      ariaLabel="Time window"
      height={32}
    />
  );

  // A failed fetch must never render zeros, which would read as "all clear".
  if (error && !isPending) {
    const failure = describeReadFailure(error);
    return (
      <Stack spacing={2.5} sx={{ mt: { xs: 1.5, sm: 2 }, px: { xs: 1, md: 0 } }}>
        <EmptyState
          icon="error"
          title={failure.title}
          description={failure.detail}
          hint={failure.hint}
          cta={<EmptyStateCta label="Retry" onClick={refetch} />}
        />
      </Stack>
    );
  }

  return (
    <Stack spacing={2.5} sx={{ mt: { xs: 1.5, sm: 2 }, px: { xs: 1, md: 0 } }}>
      <KpiStrip kpis={kpis} window={window} isPending={isPending} />

      <PinnedApps
        pinned={pinned}
        summaries={apps}
        allAppNames={appNames}
        shareLink={shareLink}
        isPending={isPending}
        onPin={pin}
        onUnpin={unpin}
      />

      <Box>
        <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center', flexWrap: 'wrap', mb: 1.5 }}>
          <SectionHeading>NEEDS ATTENTION</SectionHeading>
          <Typography sx={{ fontSize: 12, color: 'text.secondary', flexGrow: 1 }}>
            {apps.length} {apps.length === 1 ? 'application' : 'applications'} deployed in this window
          </Typography>
          {windowSelector}
        </Stack>
        <AttentionCards summaries={attention} isPending={isPending} />
      </Box>

      {!isPending && apps.length === 0 ? (
        <EmptyState
          icon="inbox"
          title="No deployments in this window"
          description="Widen the window, or wait for the next deployment to land."
        />
      ) : (
        !isPending && <AllApplications summaries={apps} />
      )}
    </Stack>
  );
};
