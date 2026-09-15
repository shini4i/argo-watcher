import { describe, expect, it } from 'vitest';
import { monogramSwatch } from './monogram';
import { tokens } from './tokens';

describe('monogramSwatch', () => {
  it('returns the same swatch for the same name', () => {
    expect(monogramSwatch('checkout-api', false)).toBe(monogramSwatch('checkout-api', false));
  });

  // Hue stability holds only while both lists pair index for index. Appending to one
  // alone changes the modulo per mode and reshuffles every app's dark hue.
  it('pairs the light and dark palettes index for index', () => {
    expect(tokens.monogramSwatchesDark).toHaveLength(tokens.monogramSwatches.length);
  });

  it('keeps the hue when the theme is toggled', () => {
    for (const name of ['checkout-api', 'payments', 'cart', 'billing', 'search']) {
      const light = monogramSwatch(name, false);
      const dark = monogramSwatch(name, true);
      const index = tokens.monogramSwatches.findIndex(swatch => swatch.fg === light.fg);

      expect(index).toBeGreaterThanOrEqual(0);
      expect(tokens.monogramSwatchesDark[index]).toBe(dark);
    }
  });

  it('only ever returns a defined swatch', () => {
    const names = ['', '   ', 'a', 'ю', '🙂', 'a'.repeat(500), 'checkout-api', 'payments_service'];

    for (const name of names) {
      expect(tokens.monogramSwatches).toContain(monogramSwatch(name, false));
      expect(tokens.monogramSwatchesDark).toContain(monogramSwatch(name, true));
    }
  });

  it('spreads names across the palette', () => {
    const picked = new Set(
      Array.from({ length: 50 }, (_, i) => monogramSwatch(`app-${i}`, false).fg),
    );

    expect(picked.size).toBeGreaterThan(1);
  });
});
