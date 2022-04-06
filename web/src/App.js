import React from "react";
import { BrowserRouter, Routes, Route } from "react-router-dom";
import RecentTasks from "./Components/RecentTasks";
import HistoryTasks from "./Components/HistoryTasks";
import Navbar from "./Components/Navbar";
import Box from "@mui/material/Box";

function App() {
  return (
      <BrowserRouter>
        <Box>
          <Navbar/>
          <Routes>
            <Route path="/" element={<RecentTasks />} />
            <Route path="/history" element={<HistoryTasks />} />
            <Route path="*" element={<Box sx={{textAlign: 'center'}}>Page not found 404</Box>} />
          </Routes>
        </Box>
      </BrowserRouter>
  );
}

export default App;
