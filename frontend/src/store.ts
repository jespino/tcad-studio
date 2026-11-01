import { create } from 'zustand';
import type { Folder } from './api';

interface State {
  /** the open folder (null: the welcome screen) */
  folder: Folder | null;
  /** the first answer arrived (no welcome-screen flash before it) */
  loaded: boolean;
  setFolder: (f: Folder | null) => void;
  toast: { id: number; text: string } | null;
  showToast: (text: string) => void;
}

let toastId = 0;

export const useStore = create<State>((set) => ({
  folder: null,
  loaded: false,
  setFolder: (folder) => set({ folder, loaded: true }),
  toast: null,
  showToast: (text) => {
    const id = ++toastId;
    set({ toast: { id, text } });
    setTimeout(() => set((s) => (s.toast?.id === id ? { toast: null } : s)), 6000);
  },
}));
