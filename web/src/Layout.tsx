import React from "react";
import { Outlet } from "react-router-dom";
import MuiAlert, { AlertProps } from "@mui/material/Alert";
import Box from "@mui/material/Box";

import Navbar from "./Components/Navbar";
import { useErrorContext } from "./ErrorContext";

interface Message {
  status: "success" | "error" | "warning" | "info";
  message: string;
}

interface ErrorContextState {
  messages: Message[];
  clearMessage: (status: string, message: string) => void;
}

const Alert = React.forwardRef<HTMLDivElement, AlertProps>(
  (props, ref) => {
    return <MuiAlert elevation={6} ref={ref} variant="filled" {...props} />;
  }
);

const Layout: React.FC = () => {
  const errorContext = useErrorContext();
  const { messages, clearMessage }: ErrorContextState = errorContext || {
    messages: [],
    clearMessage: () => {},
  };

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
              sx={{ width: "100%", borderRadius: 0 }}
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
