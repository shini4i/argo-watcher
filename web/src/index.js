import React from 'react';
import { createRoot } from 'react-dom/client';
import CssBaseline from '@mui/material/CssBaseline';
import '@fontsource/roboto/300.css';
import '@fontsource/roboto/400.css';
import '@fontsource/roboto/500.css';
import '@fontsource/roboto/700.css';
import './index.css';
import App from './App';

const root = document.getElementById('root');
createRoot(root).render(
  <React.StrictMode>
    <CssBaseline />
    <App />
  </React.StrictMode>
);
