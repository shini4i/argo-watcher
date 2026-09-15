import { tokens } from '../../../theme/tokens';

/** `neutral` covers a reason that reports an outcome rather than a failure. */
export type FailureTone = 'error' | 'neutral';

export interface FailureTonePalette {
  /** Icon, left edge and action colour. */
  readonly accent: string;
  /** Backdrop of the reason panel itself. */
  readonly panelBg: string;
  /** Tint of the table row that owns the panel. */
  readonly rowBg: string;
  /** Headline text. */
  readonly ink: string;
  /** Raw reason text under the headline. */
  readonly detailInk: string;
  /** Panel border, and the backdrop of the raw-reason block. */
  readonly border: string;
  readonly codeBg: string;
}

const ERROR_TONE = {
  light: {
    accent: tokens.statusFailedFg,
    panelBg: tokens.statusFailedBg,
    rowBg: tokens.rowFailedBg,
    ink: tokens.failureInk,
    detailInk: tokens.failureInkSecondary,
    border: tokens.failurePanelBorder,
    codeBg: tokens.failurePanelCodeBg,
  },
  dark: {
    accent: tokens.statusFailedFgDark,
    panelBg: tokens.statusFailedBgDark,
    rowBg: tokens.rowFailedBgDark,
    ink: tokens.failureInkDark,
    detailInk: tokens.failureInkSecondaryDark,
    border: tokens.failurePanelBorderDark,
    codeBg: tokens.failurePanelCodeBgDark,
  },
} as const;

// A neutral reason is not an alarm: no row tint, no coloured edge, and the
// page's own divider for the border.
const NEUTRAL_TONE = {
  light: {
    accent: tokens.statusInfoFg,
    panelBg: tokens.statusInfoBg,
    rowBg: 'transparent',
    ink: tokens.textPrimary,
    detailInk: tokens.textSecondary,
    border: tokens.divider,
    codeBg: 'rgba(0, 0, 0, 0.04)',
  },
  dark: {
    accent: tokens.statusInfoFgDark,
    panelBg: tokens.statusInfoBgDark,
    rowBg: 'transparent',
    ink: tokens.textPrimaryDark,
    detailInk: tokens.textSecondaryDark,
    border: tokens.dividerDark,
    codeBg: 'rgba(255, 255, 255, 0.06)',
  },
} as const;

/**
 * @description Resolves the colours a status-reason panel draws with. The list
 * row and the detail page render the same reason, so they read their palette
 * from here rather than each pairing tone with palette mode on its own.
 * @param tone whether the reason reports a failure or an outcome
 * @param isDark whether the active palette mode is dark
 */
export const failureTonePalette = (tone: FailureTone, isDark: boolean): FailureTonePalette =>
  (tone === 'error' ? ERROR_TONE : NEUTRAL_TONE)[isDark ? 'dark' : 'light'];
