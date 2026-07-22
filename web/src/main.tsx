import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { App } from './App';
import { bootstrapAuth } from './auth/authProvider';
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

// Process any OIDC authorization-code callback BEFORE mounting React. The
// `?code=...&state=...` params must be consumed while still on the URL — the
// router's index redirect strips them as soon as the tree mounts — so handling
// the callback first is what prevents a post-login redirect loop. When OIDC is
// disabled the bootstrap resolves immediately, so auth-less deployments render
// with no added delay. `finally` guarantees the app still renders if it throws.
bootstrapAuth().finally(renderApp);
