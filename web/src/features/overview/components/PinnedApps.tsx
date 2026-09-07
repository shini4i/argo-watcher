import { useState } from 'react';
import { Autocomplete, Box, Button, IconButton, Skeleton, Stack, TextField, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import PushPinIcon from '@mui/icons-material/PushPin';
import AddIcon from '@mui/icons-material/Add';
import LinkIcon from '@mui/icons-material/Link';
import { Link as RouterLink } from 'react-router-dom';
import { tokens } from '../../../theme/tokens';
import { useCopyToClipboard } from '../../../shared/hooks/useCopyToClipboard';
import { describeTaskStatus } from '../../tasks/utils/statusPresentation';
import { deriveAppState, type AppState } from '../deriveOverview';
import type { AppSummary } from '../types';

interface PinnedAppsProps {
  readonly pinned: readonly string[];
  readonly summaries: readonly AppSummary[];
  readonly allAppNames: readonly string[];
  readonly shareLink: string;
  readonly isPending: boolean;
  readonly onPin: (app: string) => void;
  readonly onUnpin: (app: string) => void;
}

const TILE_HEIGHT = 62;

const STATE_TEXT: Readonly<Record<AppState, string>> = {
  failing: 'Failing',
  running: 'Deploying',
  deployed: 'Healthy',
  idle: 'No deployments in window',
};

/** `idle` covers both "nothing happened" and "nothing landed"; say which. */
const stateText = (state: AppState, summary?: AppSummary): string =>
  state === 'idle' && (summary?.total ?? 0) > 0 ? 'No successful deploy' : STATE_TEXT[state];

/**
 * A pinned app with no deployment in the window still gets a tile — that the
 * app has been quiet is itself the answer the reader wanted.
 */
const Tile = ({
  app,
  summary,
  onUnpin,
}: {
  app: string;
  summary?: AppSummary;
  onUnpin: (app: string) => void;
}) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const state: AppState = summary ? deriveAppState(summary) : 'idle';
  const descriptor = describeTaskStatus(summary?.last_status);
  const statusColor = isDark ? descriptor.pillFgDark : descriptor.pillFg;
  const stateColor = state === 'idle' ? theme.palette.text.disabled : statusColor;

  return (
    <Box
      sx={{
        border: `1px solid ${theme.palette.divider}`,
        borderRadius: `${tokens.radiusMd}px`,
        backgroundColor: isDark ? tokens.surfaceDark : tokens.surface,
        padding: 1.25,
        height: TILE_HEIGHT,
        display: 'flex',
        alignItems: 'flex-start',
        gap: 1,
        minWidth: 0,
      }}
    >
      <Box
        aria-hidden
        sx={{ width: 8, height: 8, borderRadius: '50%', backgroundColor: stateColor, mt: 0.75, flexShrink: 0 }}
      />
      <Box sx={{ minWidth: 0, flexGrow: 1 }}>
        <Box
          component={RouterLink}
          to={`/?app=${encodeURIComponent(app)}`}
          sx={{
            display: 'block',
            fontSize: 12.5,
            fontWeight: 600,
            color: 'text.primary',
            textDecoration: 'none',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            '&:hover': { textDecoration: 'underline' },
          }}
        >
          {app}
        </Box>
        <Typography sx={{ fontSize: 11, color: stateColor }}>{stateText(state, summary)}</Typography>
      </Box>
      <IconButton
        aria-label={`Unpin ${app}`}
        size="small"
        onClick={() => onUnpin(app)}
        sx={{ padding: 0.25, flexShrink: 0 }}
      >
        <PushPinIcon sx={{ fontSize: 14 }} />
      </IconButton>
    </Box>
  );
};

const PinPicker = ({
  allAppNames,
  pinned,
  onPin,
}: {
  allAppNames: readonly string[];
  pinned: readonly string[];
  onPin: (app: string) => void;
}) => {
  const theme = useTheme();
  const [open, setOpen] = useState(false);
  const options = allAppNames.filter(name => !pinned.includes(name));

  if (!open) {
    return (
      <Box
        component="button"
        type="button"
        onClick={() => setOpen(true)}
        sx={{
          border: `1px dashed ${theme.palette.divider}`,
          borderRadius: `${tokens.radiusMd}px`,
          backgroundColor: 'transparent',
          cursor: 'pointer',
          height: TILE_HEIGHT,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          gap: 0.5,
          color: 'text.secondary',
          fontSize: 12.5,
          fontFamily: tokens.fontSans,
        }}
      >
        <AddIcon sx={{ fontSize: 16 }} />
        Pin an app
      </Box>
    );
  }

  return (
    <Autocomplete
      size="small"
      open
      options={options}
      autoHighlight
      onChange={(_event, value) => {
        if (value) {
          onPin(value);
        }
        setOpen(false);
      }}
      onBlur={() => setOpen(false)}
      // The label rather than a bare aria-label: overriding the input's props
      // costs the Autocomplete the ref it needs to find its own input.
      renderInput={params => (
        <TextField {...params} autoFocus label="Pin an application" />
      )}
      sx={{ height: TILE_HEIGHT }}
    />
  );
};

/**
 * @description Applications the reader chose to watch, always first and always
 * shown whatever the window. Pins live in this browser only; "Copy view link"
 * is how a set is shared.
 */
export const PinnedApps = ({
  pinned,
  summaries,
  allAppNames,
  shareLink,
  isPending,
  onPin,
  onUnpin,
}: PinnedAppsProps) => {
  const theme = useTheme();
  const copy = useCopyToClipboard();
  const isDark = theme.palette.mode === 'dark';
  const byApp = new Map(summaries.map(summary => [summary.app, summary]));

  return (
    <Box
      sx={{
        border: `1px solid ${isDark ? tokens.pinnedBorderDark : tokens.pinnedBorder}`,
        backgroundColor: isDark ? tokens.pinnedBgDark : tokens.pinnedBg,
        borderRadius: `${tokens.radiusMd}px`,
        padding: 2,
      }}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap', mb: 1.5 }}>
        <PushPinIcon aria-hidden sx={{ fontSize: 16, color: tokens.accent }} />
        <Typography sx={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.8px', color: tokens.accent }}>
          MY APPS
        </Typography>
        <Typography sx={{ fontSize: 12, color: 'text.secondary', flexGrow: 1 }}>
          {pinned.length} pinned · always shown first, whatever the window
        </Typography>
        <Button
          size="small"
          startIcon={<LinkIcon sx={{ fontSize: 14 }} />}
          onClick={() => void copy(shareLink, 'View link')}
          disabled={pinned.length === 0}
        >
          Copy view link
        </Button>
      </Stack>

      {pinned.length === 0 && (
        <Typography sx={{ fontSize: 12.5, color: 'text.secondary', mb: 1.5 }}>
          Pin the applications you own and they will stay at the top of this page.
        </Typography>
      )}

      <Box
        sx={{
          display: 'grid',
          gap: 1.5,
          gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr', md: 'repeat(4, 1fr)' },
        }}
      >
        {isPending
          ? pinned.map(app => <Skeleton key={app} variant="rounded" height={TILE_HEIGHT} />)
          : pinned.map(app => (
              <Tile key={app} app={app} summary={byApp.get(app)} onUnpin={onUnpin} />
            ))}
        <PinPicker allAppNames={allAppNames} pinned={pinned} onPin={onPin} />
      </Box>
    </Box>
  );
};
