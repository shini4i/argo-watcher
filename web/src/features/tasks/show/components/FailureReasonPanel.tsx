import { useState } from 'react';
import { Box, Button, Collapse, Stack, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import { tokens } from '../../../../theme/tokens';
import { useCopyToClipboard } from '../../../../shared/hooks/useCopyToClipboard';
import type { FailureSummary } from '../../utils/failureReason';
import { failureTonePalette, type FailureTone } from '../../utils/failureTone';

interface FailureReasonPanelProps {
  readonly summary: FailureSummary;
  /** `neutral` covers a reason that reports an outcome rather than a failure. */
  readonly tone: FailureTone;
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
  const isError = tone === 'error';
  const palette = failureTonePalette(tone, theme.palette.mode === 'dark');

  return (
    <Box
      sx={{
        backgroundColor: palette.panelBg,
        border: `1px solid ${palette.border}`,
        borderRadius: `${tokens.radiusMd}px`,
        padding: 2,
      }}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mb: 1 }}>
        <Typography
          sx={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.8px', color: palette.accent, flexGrow: 1 }}
        >
          {isError ? 'WHY IT FAILED' : 'STATUS REASON'}
        </Typography>
        <Button
          size="small"
          variant="outlined"
          onClick={() => void copy(summary.raw, 'Reason')}
          sx={{ color: palette.accent, borderColor: palette.accent }}
        >
          Copy reason
        </Button>
      </Stack>

      <output aria-live="polite" style={{ display: 'block' }}>
        <Typography sx={{ fontSize: 16, fontWeight: 600, color: palette.ink, overflowWrap: 'anywhere' }}>
          {summary.headline}
        </Typography>
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
        sx={{ color: palette.accent, mt: 1, px: 0 }}
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
            color: palette.ink,
            backgroundColor: palette.codeBg,
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
