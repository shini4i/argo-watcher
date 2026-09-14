import { tokens } from './tokens';

interface MonogramSwatch {
  readonly bg: string;
  readonly fg: string;
}

/** Stable non-negative index derived from a name, for picking a palette entry. */
const hashIndex = (name: string, modulo: number): number => {
  let hash = 0;
  for (let i = 0; i < name.length; i += 1) {
    hash = Math.imul(hash, 31) + (name.codePointAt(i) ?? 0);
  }
  return Math.abs(hash) % modulo;
};

/**
 * Picks an app's monogram colours. The index is chosen before the mode, so a theme
 * toggle keeps the hue — which holds only while both lists pair index for index.
 */
export const monogramSwatch = (name: string, isDark: boolean): MonogramSwatch => {
  const swatches = isDark ? tokens.monogramSwatchesDark : tokens.monogramSwatches;
  return swatches[hashIndex(name, swatches.length)];
};
