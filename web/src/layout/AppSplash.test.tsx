import { render, screen } from '@testing-library/react';
import { ThemeProvider } from '@mui/material/styles';
import { describe, expect, it } from 'vitest';
import { lightTheme } from '../theme';
import { tokens } from '../theme/tokens';
import { AppSplash } from './AppSplash';

describe('AppSplash', () => {
  it('announces the caller-supplied status through a live region', () => {
    render(<AppSplash message="Signing in…" />);

    const status = screen.getByRole('status');
    expect(status).toHaveAttribute('aria-live', 'polite');
    expect(status.textContent).toContain('Signing in');
    // Only the status line is live — the title must stay outside it.
    expect(status.textContent).not.toContain('Argo Watcher');
  });

  it('renders the neutral message used before OIDC is known', () => {
    render(<AppSplash message="Loading…" />);

    expect(screen.getByRole('status').textContent).toContain('Loading');
    expect(screen.getByRole('status').textContent).not.toContain('Signing in');
  });

  it('renders three staggered dots', () => {
    render(<AppSplash message="Loading…" />);

    const dots = screen.getAllByTestId('app-splash-dot');
    expect(dots).toHaveLength(3);

    const delays = dots.map(dot => window.getComputedStyle(dot).animationDelay);
    expect(new Set(delays).size).toBe(3);
  });

  it('stops the dots animating for a reduced-motion preference', () => {
    render(<AppSplash message="Loading…" />);

    // jsdom does not evaluate media queries, so the emitted rule is read instead
    // of the computed style. Whitespace is stripped so the assertion does not
    // depend on the CSS engine's formatting.
    const css = Array.from(document.querySelectorAll('style'))
      .map(style => style.textContent ?? '')
      .join('')
      .replace(/\s+/g, '');

    // One dot is enough: all three come from the same style callback.
    const [dot] = screen.getAllByTestId('app-splash-dot');
    const dotClass = Array.from(dot.classList).find(name => name.startsWith('css-'));
    const rule = css.match(
      new RegExp(`@media\\(prefers-reduced-motion:reduce\\)\\{\\.${dotClass}\\{([^}]*)\\}`),
    );

    expect(rule, `no reduced-motion rule emitted for .${dotClass}`).not.toBeNull();
    // With the animation off, the resting dim state is what the user sees.
    expect(rule?.[1]).toContain('animation:none');
    expect(rule?.[1]).toContain('opacity:0.6');
  });

  it('renders dark even when the surrounding app theme is light', () => {
    render(
      <ThemeProvider theme={lightTheme}>
        <AppSplash message="Loading…" />
      </ThemeProvider>,
    );

    // The white logo artwork is only legible on a dark surface.
    expect(screen.getByTestId('app-splash')).toHaveStyle({ backgroundColor: tokens.canvasDark });
    // Text colour must come from the splash's own dark theme, not the inherited
    // light-mode body colour, or the title is unreadable on the dark panel.
    expect(screen.getByTestId('app-splash-panel')).toHaveStyle({ color: tokens.textPrimaryDark });
  });

  it('covers the whole viewport with the app bar gradient', () => {
    render(<AppSplash message="Loading…" />);

    // Without the full-bleed geometry the light body shows around the panel —
    // the white screen this component exists to replace.
    expect(screen.getByTestId('app-splash')).toHaveStyle({
      position: 'fixed',
      inset: '0px',
      backgroundImage: tokens.appBarGradientDark,
    });
  });
});
