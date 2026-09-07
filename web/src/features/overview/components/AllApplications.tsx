import { useMemo, useState } from 'react';
import { Box, InputAdornment, Stack, TextField, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import SearchIcon from '@mui/icons-material/Search';
import { Link as RouterLink } from 'react-router-dom';
import { tokens } from '../../../theme/tokens';
import { formatDuration, formatRelativeTime } from '../../../shared/utils/time';
import { describeTaskStatus } from '../../tasks/utils/statusPresentation';
import { deriveAppState } from '../deriveOverview';
import type { AppSummary } from '../types';

interface AllApplicationsProps {
  readonly summaries: readonly AppSummary[];
}

const ROW_HEIGHT = 33;

const Row = ({ summary }: { summary: AppSummary }) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const descriptor = describeTaskStatus(summary.last_status);
  const state = deriveAppState(summary);
  const dotColor =
    state === 'idle'
      ? theme.palette.text.disabled
      : (isDark ? descriptor.pillFgDark : descriptor.pillFg);

  return (
    <Stack
      component={RouterLink}
      to={`/?app=${encodeURIComponent(summary.app)}`}
      direction="row"
      spacing={1.5}
      sx={{
        alignItems: 'center',
        height: ROW_HEIGHT,
        px: 1.5,
        textDecoration: 'none',
        color: 'inherit',
        borderTop: `1px solid ${theme.palette.divider}`,
        '&:hover': { backgroundColor: isDark ? tokens.rowHoverDark : tokens.rowHoverLight },
      }}
    >
      <Box
        aria-hidden
        sx={{ width: 8, height: 8, borderRadius: '50%', backgroundColor: dotColor, flexShrink: 0 }}
      />
      <Typography noWrap sx={{ fontSize: 12.5, width: 200, flexShrink: 0 }} title={summary.app}>
        {summary.app}
      </Typography>
      <Typography
        noWrap
        sx={{ fontFamily: tokens.fontMono, fontSize: 11, color: 'text.secondary', width: 90, flexShrink: 0 }}
        title={summary.project}
      >
        {summary.project}
      </Typography>
      <Typography sx={{ fontSize: 11.5, color: 'text.secondary', width: 130, flexShrink: 0 }}>
        {summary.total} runs · {summary.failed} failed
      </Typography>
      <Typography noWrap sx={{ fontSize: 11.5, color: 'text.secondary', flexGrow: 1, minWidth: 0 }}>
        {descriptor.displayLabel} {formatRelativeTime(summary.last_created)}
      </Typography>
      <Typography
        sx={{ fontFamily: tokens.fontMono, fontSize: 11.5, color: 'text.secondary', flexShrink: 0 }}
      >
        {summary.median_duration_seconds > 0 ? formatDuration(summary.median_duration_seconds) : '—'}
      </Typography>
    </Stack>
  );
};

/**
 * @description Every application with a deployment in the window, one compact
 * row each, searchable. This is the long tail the attention cards leave out.
 */
export const AllApplications = ({ summaries }: AllApplicationsProps) => {
  const theme = useTheme();
  const [query, setQuery] = useState('');

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) {
      return summaries;
    }
    return summaries.filter(
      summary =>
        summary.app.toLowerCase().includes(needle) ||
        summary.project.toLowerCase().includes(needle),
    );
  }, [summaries, query]);

  const sorted = useMemo(
    () => [...filtered].sort((a, b) => a.app.localeCompare(b.app)),
    [filtered],
  );

  return (
    <Box
      sx={{
        border: `1px solid ${theme.palette.divider}`,
        borderRadius: `${tokens.radiusMd}px`,
        overflow: 'hidden',
      }}
    >
      <Stack
        direction={{ xs: 'column', sm: 'row' }}
        spacing={1}
        sx={{ alignItems: { sm: 'center' }, px: 1.5, py: 1.25 }}
      >
        <Typography
          sx={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.8px', color: 'text.secondary' }}
        >
          ALL APPLICATIONS
        </Typography>
        <Typography sx={{ fontSize: 12, color: 'text.secondary', flexGrow: 1 }}>
          {sorted.length} of {summaries.length} in this window
        </Typography>
        <TextField
          size="small"
          value={query}
          onChange={event => setQuery(event.target.value)}
          placeholder="Filter applications…"
          slotProps={{
            htmlInput: { 'aria-label': 'Filter applications' },
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <SearchIcon sx={{ fontSize: 16, color: theme.palette.text.secondary }} />
                </InputAdornment>
              ),
              sx: { height: 30, borderRadius: `${tokens.radiusMd}px`, fontSize: 12.5 },
            },
          }}
          sx={{ minWidth: 200 }}
        />
      </Stack>

      <Box sx={{ overflowX: 'auto' }}>
        {sorted.length === 0 ? (
          <Typography
            sx={{
              fontSize: 12.5,
              color: 'text.secondary',
              px: 1.5,
              py: 2,
              borderTop: `1px solid ${theme.palette.divider}`,
            }}
          >
            No application matches “{query}”.
          </Typography>
        ) : (
          sorted.map(summary => <Row key={summary.app} summary={summary} />)
        )}
      </Box>
    </Box>
  );
};
