import { Box, ButtonBase, Stack } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import CloseIcon from '@mui/icons-material/Close';
import { tokens } from '../../../theme/tokens';

export interface FilterChipDescriptor {
  readonly key: string;
  readonly labelPrefix?: string;
  readonly labelValue: string;
  readonly onRemove: () => void;
}

interface ActiveFilterBarProps {
  readonly chips: ReadonlyArray<FilterChipDescriptor>;
  readonly onClearAll?: () => void;
}

const FilterChip = ({ chip }: { chip: FilterChipDescriptor }) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const bg = isDark ? tokens.accentSoftDark : tokens.accentSoft;
  // The light-mode chip uses primary text on accent-soft. In dark mode, the
  // tinted bg is dark, so primary (light) text on it reads correctly without
  // any extra adjustments.
  return (
    <Stack
      direction="row"
      alignItems="center"
      spacing={0.5}
      sx={{
        height: 28,
        px: 1.25,
        borderRadius: tokens.radiusPill,
        border: `1px solid ${theme.palette.divider}`,
        backgroundColor: bg,
        color: theme.palette.text.primary,
        fontSize: 12,
      }}
    >
      {chip.labelPrefix && (
        <Box component="span" sx={{ color: theme.palette.text.secondary, fontWeight: 500 }}>
          {chip.labelPrefix}:
        </Box>
      )}
      <Box component="span" sx={{ fontWeight: 500 }}>
        {chip.labelValue}
      </Box>
      <ButtonBase
        onClick={chip.onRemove}
        aria-label={`Remove filter ${chip.labelPrefix ?? ''} ${chip.labelValue}`.trim()}
        sx={{
          ml: 0.25,
          width: 16,
          height: 16,
          borderRadius: '50%',
          color: theme.palette.text.secondary,
          '&:hover': {
            backgroundColor: isDark ? 'rgba(255,255,255,0.08)' : 'rgba(0,0,0,0.06)',
            color: theme.palette.text.primary,
          },
        }}
      >
        <CloseIcon sx={{ fontSize: 12 }} />
      </ButtonBase>
    </Stack>
  );
};

/**
 * Renders the active-filter row beneath the toolbar. Hidden entirely when no
 * filters are active; otherwise lists removable chips and a "Clear all" link.
 */
export const ActiveFilterBar = ({ chips, onClearAll }: ActiveFilterBarProps) => {
  if (chips.length === 0) {
    return null;
  }
  return (
    <Stack
      direction="row"
      spacing={1}
      alignItems="center"
      flexWrap="wrap"
      sx={{ minHeight: 44, py: 0.5 }}
    >
      {chips.map(chip => (
        <FilterChip key={chip.key} chip={chip} />
      ))}
      {onClearAll && (
        <ButtonBase
          onClick={onClearAll}
          sx={{
            ml: 'auto',
            fontSize: 12.5,
            color: 'primary.main',
            textDecoration: 'underline',
            '&:hover': { color: 'primary.dark' },
          }}
        >
          Clear all
        </ButtonBase>
      )}
    </Stack>
  );
};
