import Navbar from "./Components/Navbar";
import {Outlet} from "react-router-dom";
import React from "react";

function Layout() {
  return (
    <>
      <Navbar/>
      <Outlet />
    </>
  );
}

export default Layout;
