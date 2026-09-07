import { Box, Tooltip } from '@mui/material';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import { useTheme } from '@mui/material/styles';
import { tokens } from '../../../../theme/tokens';
import { useCopyToClipboard } from '../../../../shared/hooks/useCopyToClipboard';

interface CopyChipProps {
  readonly value: string;
  /** Names the value in the confirmation toast and the button's accessible label. */
  readonly label: string;
  readonly display?: string;
}

/**
 * @description Monospace chip that copies its value when clicked. Used for the
 * identifiers a support conversation needs verbatim.
 */
export const CopyChip = ({ value, label, display }: CopyChipProps) => {
  const theme = useTheme();
  const copy = useCopyToClipboard();
  const isDark = theme.palette.mode === 'dark';

  return (
    <Tooltip title={`Copy ${label.toLowerCase()}`}>
      <Box
        component="button"
        type="button"
        aria-label={`Copy ${label.toLowerCase()}`}
        onClick={event => {
          event.stopPropagation();
          void copy(value, label);
        }}
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 0.5,
          border: 0,
          cursor: 'pointer',
          height: 22,
          padding: '0 8px',
          borderRadius: `${tokens.radiusSm}px`,
          backgroundColor: isDark ? 'rgba(255,255,255,0.08)' : 'rgba(0,0,0,0.04)',
          color: 'text.secondary',
          fontFamily: tokens.fontMono,
          fontSize: 11.5,
        }}
      >
        {display ?? value}
        <ContentCopyIcon aria-hidden sx={{ fontSize: 13 }} />
      </Box>
    </Tooltip>
  );
};
