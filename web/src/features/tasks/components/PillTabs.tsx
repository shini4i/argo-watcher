import type { ReactNode } from 'react';
import { Stack } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { tokens } from '../../../theme/tokens';

export interface PillTabSpec {
  /** null is a valid id, meaning "no constraint". */
  readonly id: string | null;
  readonly label: ReactNode;
  /** Rendered after the label, e.g. a count badge. */
  readonly badge?: ReactNode;
  /** Rendered before the label, e.g. an avatar or icon. */
  readonly adornment?: ReactNode;
}

interface PillTabsProps {
  readonly tabs: ReadonlyArray<PillTabSpec>;
  readonly value: string | null;
  readonly onChange: (next: string | null) => void;
  readonly ariaLabel: string;
  readonly height?: number;
}

/**
 * @description Segmented pill group used for every one-of-N filter in the task
 * views, so the status tabs, the Mine/Everyone scope and the overview window
 * selector cannot drift apart visually.
 */
export const PillTabs = ({ tabs, value, onChange, ariaLabel, height = 36 }: PillTabsProps) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';

  return (
    <Stack
      direction="row"
      role="tablist"
      aria-label={ariaLabel}
      spacing={0.5}
      sx={{
        height,
        padding: '3px',
        borderRadius: `${tokens.radiusMd}px`,
        border: `1px solid ${theme.palette.divider}`,
        backgroundColor: isDark ? tokens.surface2Dark : tokens.surface2,
        flexShrink: 0,
      }}
    >
      {tabs.map(tab => {
        const isActive = (value ?? null) === (tab.id ?? null);
        const activeBg = isDark ? theme.palette.background.paper : tokens.surface;
        return (
          <button
            type="button"
            role="tab"
            aria-selected={isActive}
            key={tab.id ?? 'all'}
            onClick={() => onChange(tab.id)}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              border: 'none',
              padding: '4px 12px',
              borderRadius: tokens.radiusSm,
              fontSize: 12.5,
              fontFamily: tokens.fontSans,
              fontWeight: isActive ? 600 : 500,
              cursor: 'pointer',
              whiteSpace: 'nowrap',
              backgroundColor: isActive ? activeBg : 'transparent',
              color: isActive ? theme.palette.text.primary : theme.palette.text.secondary,
              boxShadow: isActive ? '0 1px 2px rgba(15, 23, 42, 0.08)' : 'none',
              transition: 'background-color 150ms ease, color 150ms ease',
            }}
          >
            {tab.adornment}
            {/* Whitespace-only text nodes generate no flex item, so this
                separates label from badge in the accessible name only. */}
            {tab.label}{tab.badge ? ' ' : null}
            {tab.badge}
          </button>
        );
      })}
    </Stack>
  );
};
