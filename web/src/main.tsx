import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { App } from './App';
import type { AuthFailure } from './auth/authFailure';
import { bootstrapAuth } from './auth/authProvider';
import { AppSplash } from './layout/AppSplash';
import { AppProviders } from './shared/providers/AppProviders';

const rootElement = document.getElementById('root');

if (!rootElement) {
  throw new Error('Root element was not found. Ensure index.html contains a div with id="root".');
}

const root = ReactDOM.createRoot(rootElement);

const renderApp = () => {
  root.render(
    <React.StrictMode>
      <BrowserRouter>
        <AppProviders>
          <App />
        </AppProviders>
      </BrowserRouter>
    </React.StrictMode>,
  );
};

/**
 * Nothing from `AppProviders` wraps the splash: the deploy-lock and ArgoCD-status
 * providers start polling the API on mount, which must not happen before a token
 * exists. `AppSplash` carries its own theme.
 *
 * @param message - status line to show beneath the logo.
 * @param error - authentication failure to show beneath the panel. Supplying one
 * makes the screen terminal: it also offers the only retry, so a misconfigured
 * provider is explained once instead of being redirected to again.
 */
const renderSplash = (message: string, error?: AuthFailure) => {
  root.render(
    <React.StrictMode>
      <AppSplash
        message={message}
        error={error}
        // A reload re-runs this whole bootstrap, which is exactly what retrying a
        // sign-in means; there is no partial state worth preserving.
        onRetry={error ? () => window.location.reload() : undefined}
      />
    </React.StrictMode>,
  );
};

// Process any OIDC authorization-code callback BEFORE mounting React-admin. The
// `?code=...&state=...` params must be consumed while still on the URL — the
// router's index redirect strips them as soon as the tree mounts — so handling
// the callback first is what prevents a post-login redirect loop. The splash
// covers that wait (an OIDC round trip is not instant) instead of leaving a blank
// page.
//
// The status line starts neutral because whether a sign-in is even involved is
// only known once `/api/v1/config` answers: an auth-less deployment waits for that
// one request and then renders, and must not be told it is signing in.
renderSplash('Loading…');
bootstrapAuth({ onOidcEnabled: () => renderSplash('Signing in…') })
  .then(failure => {
    // A reported failure keeps the app unmounted on purpose: react-admin would run
    // checkAuth on mount and head straight back to the provider that just failed,
    // which is the redirect loop this screen replaces.
    if (failure) {
      renderSplash('Sign-in failed', failure);
      return;
    }
    renderApp();
  })
  .catch(error => {
    // Only for a throw bootstrapAuth does not describe. Rendering still happens —
    // checkAuth re-runs the same path on mount — so it cannot strand the loading
    // screen.
    console.warn('[auth] Authentication bootstrap rejected; rendering anyway.', error);
    renderApp();
  });
