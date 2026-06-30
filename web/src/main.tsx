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

// Process any Keycloak login callback BEFORE mounting React. keycloak-js can only
// read the `#code=...` fragment on its first init(), and the router's index
// redirect strips that fragment as soon as the tree mounts — so initializing
// first is what prevents the post-login redirect loop. When Keycloak is disabled
// the bootstrap resolves immediately, so keycloak-less deployments render with no
// added delay. `finally` guarantees the app still renders if the bootstrap throws.
bootstrapAuth().finally(renderApp);
