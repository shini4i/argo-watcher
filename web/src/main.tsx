import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { App } from './App';
import { AppProviders } from './shared/providers/AppProviders';

const rootElement = document.getElementById('root');

if (!rootElement) {
  throw new Error('Root element was not found. Ensure index.html contains a div with id="root".');
}

ReactDOM.createRoot(rootElement).render(
  <React.StrictMode>
    <BrowserRouter>
      <AppProviders>
        <App />
      </AppProviders>
    </BrowserRouter>
  </React.StrictMode>,
);
