import { Box, Button, Stack, TableCell, TableRow, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutlined';
import { Link as RouterLink } from 'react-router-dom';
import { tokens } from '../../../theme/tokens';
import { useCopyToClipboard } from '../../../shared/hooks/useCopyToClipboard';
import type { FailureSummary } from '../utils/failureReason';

interface TaskFailureRowProps {
  readonly taskId: string;
  readonly summary: FailureSummary;
  readonly colSpan: number;
  /** `neutral` covers a reason that reports an outcome rather than a failure. */
  readonly tone?: 'error' | 'neutral';
}

/**
 * @description Full-width panel under a row that carries a status reason: the
 * extracted headline, the raw reason on one truncated line, and copy /
 * drill-down actions. Always open — the reason is why the row is being read.
 */
export const TaskFailureRow = ({ taskId, summary, colSpan, tone = 'error' }: TaskFailureRowProps) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const copy = useCopyToClipboard();
  const isError = tone === 'error';
  const accentFg = isError
    ? (isDark ? tokens.statusFailedFgDark : tokens.statusFailedFg)
    : (isDark ? tokens.statusInfoFgDark : tokens.statusInfoFg);
  const panelBg = isError
    ? (isDark ? tokens.statusFailedBgDark : tokens.statusFailedBg)
    : (isDark ? tokens.statusInfoBgDark : tokens.statusInfoBg);
  const headlineInk = isError
    ? (isDark ? tokens.failureInkDark : tokens.failureInk)
    : (isDark ? tokens.textPrimaryDark : tokens.textPrimary);
  const detailInk = isError
    ? (isDark ? tokens.failureInkSecondaryDark : tokens.failureInkSecondary)
    : (isDark ? tokens.textSecondaryDark : tokens.textSecondary);

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
          borderLeft: `4px solid ${isError ? accentFg : 'transparent'}`,
          padding: '0 12px 10px',
          backgroundColor: isError
            ? (isDark ? tokens.rowFailedBgDark : tokens.rowFailedBg)
            : 'transparent',
        }}
      >
        <Stack
          direction="row"
          spacing={1.5}
          sx={{
            alignItems: 'center',
            backgroundColor: panelBg,
            borderRadius: `${tokens.radiusMd}px`,
            padding: '8px 12px',
          }}
        >
          <ErrorOutlineIcon aria-hidden sx={{ fontSize: 16, color: accentFg, flexShrink: 0 }} />
          <Box sx={{ minWidth: 0, flexGrow: 1 }}>
            <Typography
              sx={{
                fontSize: 12.5,
                fontWeight: 600,
                color: headlineInk,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {summary.headline}
              {/* A literal space, so the accessible name does not run the two
                  together the way an ml-only gap does. */}
              {summary.location && ' '}
              {summary.location && (
                <Box component="span" sx={{ fontFamily: tokens.fontMono, fontWeight: 500, opacity: 0.85 }}>
                  {summary.location}
                </Box>
              )}
            </Typography>
            {summary.raw.trim() !== summary.headline && (
              <Typography
                sx={{
                  fontFamily: tokens.fontMono,
                  fontSize: 11,
                  color: detailInk,
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {summary.raw}
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
                color: accentFg,
                borderColor: accentFg,
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
                backgroundColor: accentFg,
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
