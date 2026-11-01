import { useEffect, useMemo, useRef, useState } from 'react';
import { EditorState, type Extension } from '@codemirror/state';
import { highlightActiveLine, keymap, lineNumbers } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { createSourceSaver, EVENT_SOURCE, SourcePanel, studioHighlight, toast, type SourceChange } from '@jespino/agentstudio';
import { api, errorText, on, type Folder, type Preview } from './api';
import { tcadLanguage } from './tcadLanguage';

const extensions: Extension[] = [
  lineNumbers(),
  highlightActiveLine(),
  history(),
  tcadLanguage,
  studioHighlight,
  EditorState.tabSize.of(2),
  keymap.of([indentWithTab, ...defaultKeymap, ...historyKeymap]),
];

/** "…/part.tcad:3:5: message" → its line, column and message. */
function parseError(error: string | undefined, file: string) {
  if (!error) return null;
  const m = /^(.*?):(\d+):(\d+): (.*)$/s.exec(error);
  if (!m) return { message: error };
  const ours = m[1].replace(/\\/g, '/').endsWith('/' + file) || m[1] === file;
  return ours ? { line: Number(m[2]), col: Number(m[3]), message: m[4] } : { message: error };
}

/**
 * The folder's models: the one the viewer shows (choosing another shows it
 * there too), saved as you type; the agent's edits appear live.
 */
export function Source({ folder, preview }: { folder: Folder; preview: Preview }) {
  const [files, setFiles] = useState<string[]>([]);
  const [selected, setSelected] = useState(preview.model);
  const [text, setText] = useState('');
  const [revision, setRevision] = useState(0);
  const current = useRef({ selected, text });
  current.current = { selected, text };
  // the file the pending save belongs to (it still goes there after a switch)
  const pending = useRef(selected);
  const saver = useMemo(() => createSourceSaver(400, (t) => api.writeSource(pending.current, t)), []);

  // the viewer switched model (the agent saved another, or the user picked one)
  useEffect(() => {
    if (preview.model) setSelected(preview.model);
  }, [preview.model]);

  useEffect(() => {
    api.sourceFiles().then(setFiles, () => {});
  }, [preview.model, folder.dir]);

  useEffect(() => {
    if (!selected) return;
    api.readSource(selected).then(
      (t) => {
        setText(t);
        setRevision((r) => r + 1);
      },
      (e) => toast(errorText(e)),
    );
  }, [selected]);

  // the file changed on disk (the agent)
  useEffect(
    () =>
      on<SourceChange>(EVENT_SOURCE, (ch) => {
        const { selected: sel, text: cur } = current.current;
        if (ch.path.replace(/\\/g, '/').endsWith('/' + sel) && ch.source !== cur) {
          saver.cancel();
          setText(ch.source);
          setRevision((r) => r + 1);
        }
      }),
    [],
  );

  const problem = selected === preview.model ? parseError(preview.error, selected) : null;
  const marks = useMemo(() => (problem && 'line' in problem && problem.line ? [{ line: problem.line, className: 'cm-error-line' }] : []), [problem?.message]);

  if (!selected) {
    return (
      <div className="panel source">
        <p className="muted small source-empty">No .tcad models in this folder yet: ask the agent for one.</p>
      </div>
    );
  }
  return (
    <SourcePanel
      value={text}
      revision={revision}
      extensions={extensions}
      files={{
        list: files.includes(selected) ? files : [selected, ...files],
        selected,
        onSelect: (f) => {
          setSelected(f);
          api.showModel(f).catch(() => {});
        },
      }}
      marks={marks}
      problem={problem}
      onEdit={(t) => {
        setText(t);
        if (pending.current !== selected) saver.flush(); // the other file's last edit
        pending.current = selected;
        saver.schedule(t);
      }}
    />
  );
}
