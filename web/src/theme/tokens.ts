/**
 * Centralised design tokens shared across the MUI palette, component overrides,
 * and one-off component primitives. New colours/spacing should land here first
 * and only then be referenced from `theme/index.ts` or component code.
 */
export const tokens = {
  // brand
  ink: '#2E3B55',
  accent: '#5B7CFA',
  accentSoft: '#EEF2FF',

  // status pill backgrounds/foregrounds (light mode)
  statusDeployedBg: '#E8F5E9',
  statusDeployedFg: '#2E7D32',
  statusFailedBg: '#FDECEA',
  statusFailedFg: '#D32F2F',
  statusRunningBg: '#FFF4E5',
  statusRunningFg: '#ED6C02',
  statusInfoBg: 'rgba(0, 0, 0, 0.05)',
  statusInfoFg: '#5A6478',

  // status pill backgrounds/foregrounds (dark mode — brighter fg, low-alpha tinted bg)
  statusDeployedBgDark: 'rgba(46, 125, 50, 0.20)',
  statusDeployedFgDark: '#81C784',
  statusFailedBgDark: 'rgba(211, 47, 47, 0.22)',
  statusFailedFgDark: '#EF5350',
  statusRunningBgDark: 'rgba(237, 108, 2, 0.22)',
  statusRunningFgDark: '#FFB74D',
  statusInfoBgDark: 'rgba(255, 255, 255, 0.10)',
  statusInfoFgDark: '#B0BEC5',

  // accent-soft for chips/highlights (dark mode mirror)
  accentSoftDark: 'rgba(91, 124, 250, 0.18)',

  // live indicator (refresh control dot + label) — mirrors deployed greens
  liveFg: '#2E7D32',
  liveFgDark: '#81C784',

  // monogram swatches (AppCell) — picked by stable hash of the app name
  monogramSwatches: [
    { bg: '#EEF2FF', fg: '#5B7CFA' }, // blue
    { bg: '#FFF4E5', fg: '#ED6C02' }, // amber
    { bg: '#E8F5E9', fg: '#2E7D32' }, // green
    { bg: '#FBEAFF', fg: '#7B1FA2' }, // purple
    { bg: '#FDECEA', fg: '#D32F2F' }, // red
  ],
  monogramSwatchesDark: [
    { bg: 'rgba(91, 124, 250, 0.20)', fg: '#A5B4FC' },
    { bg: 'rgba(237, 108, 2, 0.22)', fg: '#FDBA74' },
    { bg: 'rgba(46, 125, 50, 0.20)', fg: '#86EFAC' },
    { bg: 'rgba(123, 31, 162, 0.22)', fg: '#D8B4FE' },
    { bg: 'rgba(211, 47, 47, 0.22)', fg: '#FCA5A5' },
  ],

  // surfaces
  canvas: '#F6F7F9',
  surface: '#FFFFFF',
  surface2: '#FAFBFD',

  // dark-mode surfaces (kept here so the palette stays consistent across modes)
  canvasDark: '#0B1120',
  surfaceDark: '#15213B',
  surface2Dark: '#1A2746',

  // lines
  divider: 'rgba(46, 59, 85, 0.12)',
  dividerStrong: 'rgba(46, 59, 85, 0.22)',
  dividerDark: 'rgba(226, 232, 240, 0.12)',
  dividerStrongDark: 'rgba(226, 232, 240, 0.22)',

  // text
  textPrimary: '#1A2238',
  textSecondary: '#5A6478',
  textDisabled: '#8B94A8',
  textPrimaryDark: '#E2E8F0',
  textSecondaryDark: '#CBD5F5',
  textDisabledDark: '#64748B',

  // type
  fontSans: '"Roboto","Helvetica","Arial",sans-serif',
  fontMono: '"JetBrains Mono",ui-monospace,SFMono-Regular,Menlo,monospace',

  // radii
  radiusSm: 6,
  radiusMd: 8,
  radiusLg: 12,
  radiusPill: 999,

  // row hover (subtle wash on table rows)
  rowHoverLight: '#F9FAFC',
  rowHoverDark: 'rgba(91, 124, 250, 0.08)',
} as const;

export type DesignTokens = typeof tokens;
