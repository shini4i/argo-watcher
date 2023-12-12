import React from 'react';
import {BrowserRouter, Route, Routes} from 'react-router-dom';
import RecentTasks from './Components/RecentTasks';
import HistoryTasks from './Components/HistoryTasks';
import Layout from './Layout';
import Page404 from './Page404';
import {createTheme, lighten, ThemeProvider, CircularProgress, Box} from '@mui/material';
import {ErrorProvider} from './ErrorContext';
import TaskView from './Components/TaskView';
import {useAuth} from './auth';

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
});

function App() {
    const authenticated = useAuth()
    if (authenticated === null) return (
        <Box display="flex" justifyContent="center" alignItems="center" height="100vh">
            <CircularProgress />
        </Box>
    );
    else if (authenticated) return (
        <ThemeProvider theme={theme}>
            <ErrorProvider>
                <BrowserRouter>
                    <Routes>
                        <Route path="/" element={<Layout/>}>
                            <Route index element={<RecentTasks/>}/>
                            <Route path="/history" element={<HistoryTasks/>}/>
                            <Route path="/task/:id" element={<TaskView/>}/>
                        </Route>
                        <Route path="*" element={<Page404/>}/>
                    </Routes>
                </BrowserRouter>
            </ErrorProvider>
        </ThemeProvider>
    );
}

export default App;
