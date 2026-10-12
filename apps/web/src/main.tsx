import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';

const container = document.getElementById('root');

if (!container) {
  throw new Error('页面缺少 React 挂载节点 #root。');
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
