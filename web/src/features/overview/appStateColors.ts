import { tokens } from '../../theme/tokens';
import type { AppState } from './deriveOverview';

interface StateColors {
  readonly bg: string;
  readonly fg: string;
}

const PALETTE: Readonly<Record<AppState, { light: StateColors; dark: StateColors }>> = {
  failing: {
    light: { bg: tokens.statusFailedBg, fg: tokens.statusFailedFg },
    dark: { bg: tokens.statusFailedBgDark, fg: tokens.statusFailedFgDark },
  },
  running: {
    light: { bg: tokens.statusRunningBg, fg: tokens.statusRunningFg },
    dark: { bg: tokens.statusRunningBgDark, fg: tokens.statusRunningFgDark },
  },
  deployed: {
    light: { bg: tokens.statusDeployedBg, fg: tokens.statusDeployedFg },
    dark: { bg: tokens.statusDeployedBgDark, fg: tokens.statusDeployedFgDark },
  },
  idle: {
    light: { bg: tokens.statusInfoBg, fg: tokens.statusInfoFg },
    dark: { bg: tokens.statusInfoBgDark, fg: tokens.statusInfoFgDark },
  },
};

/**
 * @description Colours for an app's overview badge, keyed by the same state that
 * supplies its text. Colouring from last_status instead showed a "Failing" app in
 * the success green whenever its most recent task happened to deploy.
 * @param state the app's derived state
 * @param isDark whether the dark palette applies
 * @returns the background and foreground for the badge
 */
export const appStateColors = (state: AppState, isDark: boolean): StateColors =>
  isDark ? PALETTE[state].dark : PALETTE[state].light;
