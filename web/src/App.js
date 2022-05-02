import React from "react";
import { BrowserRouter, Routes, Route } from "react-router-dom";
import RecentTasks from "./Components/RecentTasks";
import HistoryTasks from "./Components/HistoryTasks";
import Layout from "./Layout";
import Page404 from "./Page404";

function App() {
  return (
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Layout />}>
            <Route index element={<RecentTasks />} />
            <Route path="/history" element={<HistoryTasks />} />
          </Route>
          <Route path="*" element={<Page404 />} />
        </Routes>
      </BrowserRouter>
  );
}

export default App;
