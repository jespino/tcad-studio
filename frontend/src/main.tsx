import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { configureStudio } from '@jespino/agentstudio';
import '@jespino/agentstudio/styles.css';
import { api, backend } from './api';
import { App } from './App';
import { useStore } from './store';
import './styles.css';

configureStudio({
  backend,
  id: 'tcad',
  noun: 'folder',
  openTitle: 'Open folder',
  newTitle: () => 'Choose or create a folder to work in',
  openHint: 'Choose the folder to work in; those holding .tcad models are marked. Double-click one to open it.',
  newHint: 'Creates a folder (or uses the current one) and opens it.',
  namePlaceholder: 'my-parts',
  // any folder opens: the agent can start the first model there
  canOpen: () => true,
  create: (dir) => api.newProject(dir),
  currentDir: () => useStore.getState().folder?.dir,
  notify: (message) => useStore.getState().showToast(message),
});

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
