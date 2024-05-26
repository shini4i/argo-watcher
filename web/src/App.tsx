import React from 'react';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { Box, CircularProgress, createTheme, lighten, ThemeOptions, ThemeProvider } from '@mui/material';

import RecentTasks from './Components/RecentTasks';
import HistoryTasks from './Components/HistoryTasks';
import Layout from './Layout';
import Page404 from './Page404';
import { ErrorProvider } from './ErrorContext';
import TaskView from './Components/TaskView';
import { AuthContext, useAuth } from './Services/Auth';
import { DeployLockProvider } from './Services/DeployLockHandler';

const theme = createTheme({
  palette: {
    primary: {
      main: '#2E3B55',
    },
    neutral: {
      main: 'gray',
    },
    reason_color: {
      main: lighten('#ff9800', 0.5),
    },
  },
  components: {
    MuiTableCell: {
      styleOverrides: {
        root: {
          padding: '12px',
        },
      },
    },
  },
} as ThemeOptions);

const App: React.FC = () => {
  const auth = useAuth();

  if (auth.authenticated === null) {
    return (
      <Box display="flex" justifyContent="center" alignItems="center" height="100vh">
        <CircularProgress />
      </Box>
    );
  }

  if (auth.authenticated) {
    return (
      <AuthContext.Provider value={auth}>
        <ThemeProvider theme={theme}>
          <ErrorProvider>
            <DeployLockProvider>
              <BrowserRouter>
                <Routes>
                  <Route path="/" element={<Layout />}>
                    <Route index element={<RecentTasks />} />
                    <Route path="/history" element={<HistoryTasks />} />
                    <Route path="/task/:id" element={<TaskView />} />
                  </Route>
                  <Route path="*" element={<Page404 />} />
                </Routes>
              </BrowserRouter>
            </DeployLockProvider>
          </ErrorProvider>
        </ThemeProvider>
      </AuthContext.Provider>
    );
  }

  return null;
};

export default App;
