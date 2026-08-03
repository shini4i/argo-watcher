import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { App } from './App';
import { bootstrapAuth } from './auth/authProvider';
import { AppSplash } from './layout/AppSplash';
import { AppProviders } from './shared/providers/AppProviders';

const rootElement = document.getElementById('root');

if (!rootElement) {
  throw new Error('Root element was not found. Ensure index.html contains a div with id="root".');
}

const root = ReactDOM.createRoot(rootElement);

/** Mounts the React application into the page root. */
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
 * Mounts the loading screen shown while authentication is resolved.
 *
 * Nothing from `AppProviders` wraps it: the deploy-lock and ArgoCD-status
 * providers start polling the API on mount, which must not happen before a token
 * exists. `AppSplash` carries its own theme.
 *
 * @param message - status line to show beneath the logo.
 */
const renderSplash = (message: string) => {
  root.render(
    <React.StrictMode>
      <AppSplash message={message} />
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
  // `catch` before `finally`: bare `finally` re-throws, leaving an unhandled
  // rejection. Rendering still happens either way — checkAuth re-runs the same
  // path on mount — so a failed bootstrap must not strand the loading screen.
  .catch(error => {
    console.warn('[auth] Authentication bootstrap rejected; rendering anyway.', error);
  })
  .finally(renderApp);
