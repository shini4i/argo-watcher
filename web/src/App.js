import React from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import RecentTasks from './Components/RecentTasks';
import HistoryTasks from './Components/HistoryTasks';
import Layout from './Layout';
import Page404 from './Page404';
import { createTheme, ThemeProvider } from '@mui/material';
import { ErrorProvider } from './ErrorContext';
import TaskView from './Components/TaskView';

const theme = createTheme({
  palette: {
    primary: {
      main: '#2E3B55',
    },
    neutral: {
      main: 'gray',
      light: '#F8F8F8',
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
});

function App() {
  return (
    <ThemeProvider theme={theme}>
      <ErrorProvider>
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
      </ErrorProvider>
    </ThemeProvider>
  );
}

export default App;
