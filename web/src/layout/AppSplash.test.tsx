import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ThemeProvider } from '@mui/material/styles';
import { describe, expect, it, vi } from 'vitest';
import type { AuthFailure } from '../auth/authFailure';
import { lightTheme } from '../theme';
import { tokens } from '../theme/tokens';
import { AppSplash } from './AppSplash';

const failure = (overrides: Partial<AuthFailure> = {}): AuthFailure => ({
  kind: 'provider_error',
  title: 'The identity provider rejected the sign-in',
  code: 'invalid_scope',
  detail: 'Invalid scopes: groups',
  hint: 'Allow openid profile email for this client.',
  ...overrides,
});

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

  it('shows no error box for an ordinary load', () => {
    render(<AppSplash message="Loading…" />);

    expect(screen.queryByTestId('app-splash-error')).toBeNull();
  });

  it('keeps the panel sized to its own contents while loading', () => {
    render(<AppSplash message="Loading…" />);

    // Stretching it to the error box's width would blow the compact loading
    // panel up to a wide, mostly empty card.
    expect(screen.getByTestId('app-splash-stack')).toHaveStyle({ alignItems: 'center' });
  });

  describe('with an authentication failure', () => {
    it('renders the error box below the logo panel', () => {
      render(<AppSplash message="Sign-in failed" error={failure()} />);

      const panel = screen.getByTestId('app-splash-panel');
      const errorBox = screen.getByTestId('app-splash-error');

      // Reading order is the visual order here: the box has to sit under the
      // panel, not replace or precede it.
      expect(panel.compareDocumentPosition(errorBox)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
      // The logo card keeps its own size — the error box below it must not
      // stretch the panel to match.
      expect(screen.getByTestId('app-splash-stack')).toHaveStyle({ alignItems: 'center' });
    });

    it('shows the title, provider code, detail, and hint', () => {
      render(<AppSplash message="Sign-in failed" error={failure()} />);

      const errorBox = screen.getByTestId('app-splash-error');
      expect(errorBox).toHaveTextContent('The identity provider rejected the sign-in');
      expect(errorBox).toHaveTextContent('invalid_scope');
      expect(errorBox).toHaveTextContent('Invalid scopes: groups');
      expect(errorBox).toHaveTextContent('Allow openid profile email for this client.');
    });

    it('announces the failure assertively', () => {
      render(<AppSplash message="Sign-in failed" error={failure()} />);

      // A terminal error must interrupt, unlike the polite progress line.
      expect(screen.getByRole('alert')).toHaveAttribute('aria-live', 'assertive');
    });

    it('omits the optional fields that were not supplied', () => {
      render(
        <AppSplash
          message="Sign-in failed"
          error={failure({ code: undefined, detail: undefined, hint: undefined, uri: undefined })}
        />,
      );

      const errorBox = screen.getByTestId('app-splash-error');
      expect(errorBox).toHaveTextContent('The identity provider rejected the sign-in');
      expect(errorBox.textContent).not.toContain('undefined');
      expect(screen.queryByRole('link')).toBeNull();
    });

    it('links to the provider documentation when the error carries a URI', () => {
      render(
        <AppSplash message="Sign-in failed" error={failure({ uri: 'https://idp/docs/err' })} />,
      );

      const link = screen.getByRole('link', { name: /more information/i });
      expect(link).toHaveAttribute('href', 'https://idp/docs/err');
      // Opened in a new tab, so the provider-supplied target must get neither the
      // opener nor the referrer.
      expect(link).toHaveAttribute('target', '_blank');
      expect(link.getAttribute('rel')).toContain('noopener');
      expect(link.getAttribute('rel')).toContain('noreferrer');
    });

    it('keeps the status line alongside the failure', () => {
      render(<AppSplash message="Sign-in failed" error={failure()} />);

      // Two different signals: the polite line says what state the app is in, the
      // assertive box says why. Losing the line leaves the panel captionless.
      expect(screen.getByRole('status').textContent).toContain('Sign-in failed');
      expect(screen.getByRole('alert')).toHaveTextContent(
        'The identity provider rejected the sign-in',
      );
    });

    it('stops the progress dots so the screen does not look like work in flight', () => {
      render(<AppSplash message="Sign-in failed" error={failure()} />);

      expect(screen.queryAllByTestId('app-splash-dot')).toHaveLength(0);
    });

    it('retries only when the user asks', async () => {
      const onRetry = vi.fn();
      render(<AppSplash message="Sign-in failed" error={failure()} onRetry={onRetry} />);

      // Nothing may happen on its own — automatic retries are the redirect loop
      // this screen exists to replace.
      expect(onRetry).not.toHaveBeenCalled();

      await userEvent.click(screen.getByRole('button', { name: /try again/i }));
      expect(onRetry).toHaveBeenCalledTimes(1);
    });

    it('hides the retry button when no handler is supplied', () => {
      render(<AppSplash message="Sign-in failed" error={failure()} />);

      expect(screen.queryByRole('button', { name: /try again/i })).toBeNull();
    });
  });
});
