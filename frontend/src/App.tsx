import { useEffect, useState } from 'react';
import { Group, Panel, Separator } from 'react-resizable-panels';
import { ProjectMenu, ProjectPicker, SourceButton, StudioLayout, TerminalPanel, toast, Toaster, useSourceOpen, Welcome } from '@jespino/agentstudio';
import { api, errorText, EVENT_PREVIEW, EVENT_PROJECT, on, type Folder, type Preview } from './api';
import { Source } from './Source';
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
        {folder && <SourceButton title="The models' source (Ctrl+2)" />}
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
      {folder && <Editor key={folder.dir} folder={folder} />}
    </div>
  );
}

function Editor({ folder }: { folder: Folder }) {
  const [url, setUrl] = useState<string | null>(null);
  const [preview, setPreview] = useState<Preview>({ model: '' });
  const showSource = useSourceOpen();
  useEffect(() => {
    api.previewURL().then(setUrl, (e) => toast(errorText(e)));
    const off = on<Preview>(EVENT_PREVIEW, setPreview);
    api.preview().then(setPreview, () => {});
    return off;
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
      <Group orientation="horizontal">
        <Panel id="preview" minSize={280}>
          <div className="panel preview">{url && <iframe title="3D preview" src={url} />}</div>
        </Panel>
        {showSource && (
          <>
            <Separator className="sep-v" />
            <Panel id="source" defaultSize="45" minSize={260}>
              <Source folder={folder} preview={preview} />
            </Panel>
          </>
        )}
      </Group>
    </StudioLayout>
  );
}
