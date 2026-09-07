import { useState } from 'react';
import { Box, Button, Collapse, Stack, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import { tokens } from '../../../../theme/tokens';
import { useCopyToClipboard } from '../../../../shared/hooks/useCopyToClipboard';
import type { FailureSummary } from '../../utils/failureReason';

interface FailureReasonPanelProps {
  readonly summary: FailureSummary;
  /** `neutral` covers a reason that reports an outcome rather than a failure. */
  readonly tone: 'error' | 'neutral';
}

/**
 * @description First content block of the detail page when the task carries a
 * status reason: the extracted headline, and the raw text from Argo CD behind a
 * disclosure. The raw text is always reachable — extraction is best-effort.
 */
export const FailureReasonPanel = ({ summary, tone }: FailureReasonPanelProps) => {
  const theme = useTheme();
  const copy = useCopyToClipboard();
  const [open, setOpen] = useState(false);
  const isDark = theme.palette.mode === 'dark';
  const isError = tone === 'error';

  const accentFg = isError
    ? (isDark ? tokens.statusFailedFgDark : tokens.statusFailedFg)
    : (isDark ? tokens.statusInfoFgDark : tokens.statusInfoFg);
  const bg = isError
    ? (isDark ? tokens.statusFailedBgDark : tokens.statusFailedBg)
    : (isDark ? tokens.statusInfoBgDark : tokens.statusInfoBg);
  const ink = isError
    ? (isDark ? tokens.failureInkDark : tokens.failureInk)
    : (isDark ? tokens.textPrimaryDark : tokens.textPrimary);

  return (
    <Box
      sx={{
        backgroundColor: bg,
        border: `1px solid ${
          isError
            ? (isDark ? tokens.failurePanelBorderDark : tokens.failurePanelBorder)
            : theme.palette.divider
        }`,
        borderRadius: `${tokens.radiusMd}px`,
        padding: 2,
      }}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mb: 1 }}>
        <Typography
          sx={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.8px', color: accentFg, flexGrow: 1 }}
        >
          {isError ? 'WHY IT FAILED' : 'STATUS REASON'}
        </Typography>
        <Button
          size="small"
          variant="outlined"
          onClick={() => void copy(summary.raw, 'Reason')}
          sx={{ color: accentFg, borderColor: accentFg }}
        >
          Copy reason
        </Button>
      </Stack>

      <output aria-live="polite" style={{ display: 'block' }}>
        <Typography sx={{ fontSize: 16, fontWeight: 600, color: ink, overflowWrap: 'anywhere' }}>
          {summary.headline}
        </Typography>
        {summary.location && (
          <Typography sx={{ fontFamily: tokens.fontMono, fontSize: 14, color: ink, mt: 0.5 }}>
            {summary.location}
          </Typography>
        )}
      </output>

      <Button
        size="small"
        onClick={() => setOpen(previous => !previous)}
        aria-expanded={open}
        endIcon={
          <ExpandMoreIcon
            sx={{ transform: open ? 'rotate(180deg)' : 'none', transition: 'transform 150ms' }}
          />
        }
        sx={{ color: accentFg, mt: 1, px: 0 }}
      >
        Full reason from Argo CD ({summary.lineCount} {summary.lineCount === 1 ? 'line' : 'lines'})
      </Button>
      <Collapse in={open}>
        <Typography
          component="pre"
          sx={{
            fontFamily: tokens.fontMono,
            fontSize: 11.5,
            lineHeight: 1.6,
            color: ink,
            backgroundColor: isError
              ? (isDark ? tokens.failurePanelCodeBgDark : tokens.failurePanelCodeBg)
              : (isDark ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)'),
            borderRadius: `${tokens.radiusSm}px`,
            padding: 1.5,
            margin: 0,
            whiteSpace: 'pre-wrap',
            overflowX: 'auto',
          }}
        >
          {summary.raw}
        </Typography>
      </Collapse>
    </Box>
  );
};
