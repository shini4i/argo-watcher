import React from 'react';
import ReactDOM from 'react-dom';
// CSS reset for multi-browser support
import CssBaseline from '@mui/material/CssBaseline';
// MUI library default font
import '@fontsource/roboto/300.css';
import '@fontsource/roboto/400.css';
import '@fontsource/roboto/500.css';
import '@fontsource/roboto/700.css';
// Custom CSS
import './index.css';
// Application
import App from './App';

// Start the application
ReactDOM.render(
  <React.StrictMode>
    <CssBaseline />
    <App />
  </React.StrictMode>,
  document.getElementById('root'),
);
