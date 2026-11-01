import { useEffect, useState } from 'react';
import { ProjectMenu, ProjectPicker, StudioLayout, TerminalPanel, toast, Toaster, Welcome } from '@jespino/agentstudio';
import { api, errorText, EVENT_PROJECT, on, type Folder } from './api';
import { useStore } from './store';

function Cube() {
  return (
    <svg className="cube" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 2.8 20.5 7.5v9L12 21.2 3.5 16.5v-9z" />
      <path d="M3.5 7.5 12 12.2l8.5-4.7M12 12.2v9" />
    </svg>
  );
}

export function App() {
  const folder = useStore((s) => s.folder);
  const loaded = useStore((s) => s.loaded);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const off = on<Folder | null>(EVENT_PROJECT, (f) => useStore.getState().setFolder(f));
    api.currentProject().then(
      (f) => useStore.getState().setFolder(f),
      (e) => setError(errorText(e)),
    );
    return off;
  }, []);

  useEffect(() => {
    document.title = folder ? `${folder.name} · TCAD Studio` : 'TCAD Studio';
  }, [folder]);

  return (
    <div className="app">
      <header className="toolbar">
        <span className="brand">
          <Cube />
          TCAD Studio
        </span>
        {folder && <ProjectMenu project={folder} showFile={false} />}
        {folder && <span className="muted small ellipsis folder-dir">{folder.dir}</span>}
      </header>
      {error && <div className="banner error">{error}</div>}
      <Toaster />
      <ProjectPicker />
      {loaded && !folder && !error && (
        <Welcome title="TCAD Studio">
          <p>
            Open a folder to work in it. TCAD Studio puts the TCAD skill and the agent's instructions there: ask the agent for parts in the right-hand
            panel and you'll see them on the left, in 3D, as soon as it saves them.
          </p>
        </Welcome>
      )}
      {/* keyed by folder: another folder gets a new viewer and terminal */}
      {folder && <Editor key={folder.dir} />}
    </div>
  );
}

function Editor() {
  const [url, setUrl] = useState<string | null>(null);
  useEffect(() => {
    api.previewURL().then(setUrl, (e) => toast(errorText(e)));
  }, []);
  return (
    <StudioLayout
      agentSize="40"
      mainMinSize={280}
      agent={
        <TerminalPanel
          intro={
            <>
              <p>Start an agent in the folder and ask it to design or change parts.</p>
              <p className="muted small">
                It gets the TCAD skill, an AGENTS.md with instructions and the <code>tcad</code> tool. The preview on the left shows every file it saves.
              </p>
            </>
          }
        />
      }
    >
      <div className="panel preview">{url && <iframe title="3D preview" src={url} />}</div>
    </StudioLayout>
  );
}
