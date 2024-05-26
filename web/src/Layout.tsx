import React from 'react';
import { Outlet } from 'react-router-dom';
import MuiAlert, { AlertProps } from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Navbar from './Components/Navbar';
import { useErrorContext, ErrorContextType } from './ErrorContext'; // `ErrorContextType` imported here

const Alert = React.forwardRef<HTMLDivElement, AlertProps>((props, ref) => (
  <MuiAlert elevation={6} ref={ref} variant="filled" {...props} />
));

const Layout: React.FC = () => {
  const { messages, clearMessage }: ErrorContextType = useErrorContext(); // `ErrorContextType` used here

  return (
    <>
      <Navbar />
      <Box sx={{ mb: 2 }}>
        {messages.length > 0 &&
          messages.map(message => (
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
          ))}
      </Box>
      <Outlet />
    </>
  );
};

export default Layout;
