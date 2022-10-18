import Navbar from './Components/Navbar';
import { Outlet } from 'react-router-dom';
import React from 'react';
import { useErrorContext } from './ErrorContext';
import MuiAlert from '@mui/material/Alert';
import Box from '@mui/material/Box';

const Alert = React.forwardRef(function Alert(props, ref) {
  return <MuiAlert elevation={6} ref={ref} variant="filled" {...props} />;
});

function Layout() {
  const { messages, clearMessage } = useErrorContext();

  return (
    <>
      <Navbar />
      <Box sx={{ mb: 2 }}>
        {messages.length > 0 &&
          messages.map(message => {
            return (
              <Alert
                onClose={() => {
                  clearMessage(message.status, message.message);
                }}
                severity={message.status}
                sx={{ width: '100%', borderRadius: 0 }}
                key={`${message.status} ${message.message}`}
              >
                {message.message}
              </Alert>
            );
          })}
      </Box>
      <Outlet />
    </>
  );
}

export default Layout;
