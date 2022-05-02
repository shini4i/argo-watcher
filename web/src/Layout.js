import Box from "@mui/material/Box";
import Navbar from "./Components/Navbar";
import {Outlet} from "react-router-dom";
import React from "react";

function Layout() {
  return (
      <Box>
        <Navbar/>
        <Outlet />
      </Box>
  );
}

export default Layout;
