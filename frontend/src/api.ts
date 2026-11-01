// Backend transport (agentstudio's): in the desktop app, calls go through the
// Wails runtime; in a browser (tcad studio / Vite dev), through HTTP +
// server-sent events. Both reach the same Go service (internal/studio.Studio).
import { baseApi, createBackend } from '@jespino/agentstudio';

export const backend = createBackend({
  service: 'github.com/jespino/tcad-studio/internal/studio.Studio',
  noBackend: 'No TCAD Studio backend is reachable. In a browser, run `make dev` (it starts `tcad studio` and this UI); the desktop app has it built in.',
});

export const { on } = backend;
export { errorText } from '@jespino/agentstudio';

/** The open folder. */
export interface Folder {
  path: string;
  dir: string;
  name: string;
}

export const api = {
  ...baseApi(backend.call),
  currentProject: () => backend.call<Folder | null>('CurrentProject'),
  newProject: (dir: string) => backend.call<Folder>('NewProject', dir),
  /** where the viewer of the open folder is served */
  previewURL: () => backend.call<string>('PreviewURL'),
};

export { EVENT_PROJECT } from '@jespino/agentstudio';
