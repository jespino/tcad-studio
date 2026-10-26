# TCAD project

This folder is a **TCAD Studio** project. You are the agent running in the
right-hand panel; on the left, the user sees a live 3D preview of the `.tcad`
models in this folder and its subfolders.

## How it works

- Models are written in the TCAD language. Before your first model, read the
  `tcad` skill: `.agents/skills/tcad/SKILL.md` and its reference
  (`reference.md`), and the recipes in `patterns.md` when there are fits,
  fasteners, text or repetitions. (`.claude/skills/tcad/` holds the same files.)
- Each part is a `.tcad` file. A project with several parts goes in its own
  subfolder (for example `station/base.tcad`, `station/lid.tcad`). Use
  lowercase file names without spaces.
- The preview recompiles on save and **jumps by itself to the file you just
  created or saved**, so the user sees every change right away. Don't run
  `tcad watch`: the preview is already open. The user can also pick another
  model in the preview's dropdown.
- The `tcad` tool is on this terminal's PATH: `tcad check`, `tcad info`,
  `tcad render` (a PNG: look at it to check the result), `tcad build`
  (STL) and `tcad import` (STL → TCAD). Keep test PNGs and STLs out of the
  project folder (for example in `/tmp`) unless the user asks for them.

## What the user is looking at

After every build, TCAD Studio writes **`.tcad/preview.json`**:

```json
{
  "model": "station/lid.tcad",
  "ok": false,
  "error": "/path/to/the/folder/station/lid.tcad:12:3: unknown shape \"bx\" (did you mean \"box\"?)",
  "size": [80, 60, 4],
  "params": [{ "name": "width", "value": 90, "default": 80, "overridden": true }],
  "overrides": { "width": "90" }
}
```

- `model` is the model open in the preview (relative to this folder): when the
  user talks about "this part" or "what I'm looking at", it's that one.
- `ok`/`error` is the result of the last build of that model.
- `overrides` are the values the user changed in the params panel without
  touching the file. If they ask to keep them, write them as the `param`'s
  value in the `.tcad`.

## Rules

1. Read a file before editing it; keep the user's names, comments and style.
2. After every change, check that it compiles (`tcad check`, or
   `.tcad/preview.json` if it is the model being viewed), and check it visually
   with `tcad render` when the shape changes.
3. Expose as `param` (with its `// [min:max] description` comment, or
   `in [...]` for closed sets of values) the dimensions the user will want to
   adjust: they show up as controls in the preview.
4. Don't delete the user's files unless they ask you to.
