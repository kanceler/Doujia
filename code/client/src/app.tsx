import React from 'react';
import { Route, Routes } from 'react-router-dom';

import Layout from './components/Layout';
import HomePage from './pages/HomePage/HomePage';
import RunCreatePage from './pages/RunCreatePage/RunCreatePage';
import RunWorkspacePage from './pages/RunWorkspacePage/RunWorkspacePage';
import WebInjectPage from './pages/WebInjectPage/WebInjectPage';
import TemplatesPage from './pages/TemplatesPage/TemplatesPage';
import RunDetailPage from './pages/RunDetailPage/RunDetailPage';
import DemoPage from './pages/DemoPage/DemoPage';
import NotFound from './pages/NotFound/NotFound';

const RoutesComponent = () => {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<HomePage />} />
        <Route path="run/create" element={<RunCreatePage />} />
        <Route path="run/:runId/workspace" element={<RunWorkspacePage />} />
        <Route path="run/:runId/inject" element={<WebInjectPage />} />
        <Route path="run/:runId/detail" element={<RunDetailPage />} />
        <Route path="templates" element={<TemplatesPage />} />
        <Route path="demo" element={<DemoPage />} />
      </Route>
      <Route path="*" element={<NotFound />} />
    </Routes>
  );
};

export default RoutesComponent;
