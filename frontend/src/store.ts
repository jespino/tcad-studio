import { create } from 'zustand';
import type { Folder } from './api';

interface State {
  /** the open folder (null: the welcome screen) */
  folder: Folder | null;
  /** the first answer arrived (no welcome-screen flash before it) */
  loaded: boolean;
  setFolder: (f: Folder | null) => void;
}

export const useStore = create<State>((set) => ({
  folder: null,
  loaded: false,
  setFolder: (folder) => set({ folder, loaded: true }),
}));
