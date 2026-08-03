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
