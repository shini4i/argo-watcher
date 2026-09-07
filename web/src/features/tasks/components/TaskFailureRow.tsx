import { Box, Button, Stack, TableCell, TableRow, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutlined';
import { Link as RouterLink } from 'react-router-dom';
import { tokens } from '../../../theme/tokens';
import { useCopyToClipboard } from '../../../shared/hooks/useCopyToClipboard';
import type { FailureSummary } from '../utils/failureReason';
import { failureTonePalette, type FailureTone } from '../utils/failureTone';

interface TaskFailureRowProps {
  readonly taskId: string;
  readonly summary: FailureSummary;
  readonly colSpan: number;
  /** `neutral` covers a reason that reports an outcome rather than a failure. */
  readonly tone?: FailureTone;
}

/**
 * @description Full-width panel under a row that carries a status reason: the
 * extracted headline, the raw reason on one truncated line, and copy /
 * drill-down actions. Always open — the reason is why the row is being read.
 */
export const TaskFailureRow = ({ taskId, summary, colSpan, tone = 'error' }: TaskFailureRowProps) => {
  const theme = useTheme();
  const copy = useCopyToClipboard();
  const isError = tone === 'error';
  const palette = failureTonePalette(tone, theme.palette.mode === 'dark');

  return (
    <TableRow>
      <TableCell
        colSpan={colSpan}
        sx={{
          // The next task's own top border closes this block off, so the row
          // and its panel read as one unit. The left edge has to match the
          // owner row's, which only an attention-worthy status colours.
          borderTop: 'none',
          borderBottom: 'none',
          borderLeft: `4px solid ${isError ? palette.accent : 'transparent'}`,
          padding: '0 12px 10px',
          backgroundColor: palette.rowBg,
        }}
      >
        <Stack
          direction="row"
          spacing={1.5}
          sx={{
            alignItems: 'center',
            backgroundColor: palette.panelBg,
            borderRadius: `${tokens.radiusMd}px`,
            padding: '8px 12px',
          }}
        >
          <ErrorOutlineIcon aria-hidden sx={{ fontSize: 16, color: palette.accent, flexShrink: 0 }} />
          <Box sx={{ minWidth: 0, flexGrow: 1 }}>
            <Typography
              sx={{
                fontSize: 12.5,
                fontWeight: 600,
                color: palette.ink,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {summary.headline}
            </Typography>
            {summary.detail && (
              <Typography
                sx={{
                  fontFamily: tokens.fontMono,
                  fontSize: 11,
                  color: palette.detailInk,
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {summary.detail}
              </Typography>
            )}
          </Box>
          <Stack direction="row" spacing={0.75} sx={{ flexShrink: 0 }}>
            <Button
              size="small"
              variant="outlined"
              onClick={event => {
                event.stopPropagation();
                void copy(summary.raw, 'Reason');
              }}
              sx={{
                color: palette.accent,
                borderColor: palette.accent,
                minWidth: 0,
                // MUI shouts button labels by default; the panel is dense and
                // sits inside a table row, where uppercase reads as an alarm.
                textTransform: 'none',
                fontSize: 12,
                paddingY: 0.15,
              }}
            >
              Copy
            </Button>
            <Button
              component={RouterLink}
              to={`/task/${encodeURIComponent(taskId)}`}
              size="small"
              variant="contained"
              onClick={event => event.stopPropagation()}
              sx={{
                backgroundColor: palette.accent,
                minWidth: 0,
                whiteSpace: 'nowrap',
                textTransform: 'none',
                fontSize: 12,
                paddingY: 0.15,
                boxShadow: 'none',
              }}
            >
              Full reason
            </Button>
          </Stack>
        </Stack>
      </TableCell>
    </TableRow>
  );
};
