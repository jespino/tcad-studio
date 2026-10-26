---
name: tcad
description: Design printable 3D models with the TCAD language (primitives such as box, cylinder, sphere, cone, torus, text... combined as solids and holes), generate STL files and PNG previews, and import existing STL files to convert them to TCAD, with the `tcad` tool. Use it when the user asks to create, modify, review, convert or explain a 3D model, a part to 3D print, a .tcad file or an STL.
---

# TCAD: 3D modeling with solids and holes

TCAD is a declarative language for describing 3D parts with solids and
holes: you place primitives (boxes, cylinders, spheres...), mark some of
them as **holes** (`hole`), and when you **group** them (`group { }`) the holes
are subtracted from the solids. The `tcad` tool turns the file into an STL (to
print) or a PNG (to check the result).

- Full reference of the language and of every primitive: [reference.md](reference.md)
- Design recipes (boxes, lids, screws, polar arrays, text...): [patterns.md](patterns.md)

Read `reference.md` the first time you write a model in a conversation, and
`patterns.md` when the part has fits, fasteners, repetitions or text.

## The tool

```bash
tcad check  model.tcad                  # only validates (errors with line:column)
tcad info   model.tcad                  # size, bbox, volume, watertight?, params
tcad render model.tcad -o view.png      # 2x2 sheet: iso, front, right, top
tcad render model.tcad -view iso-below -size 700 -o below.png
tcad build  model.tcad -o part.stl      # binary STL (-ascii for text)
tcad import part.stl -o part.tcad       # converts an STL into TCAD
tcad watch  model.tcad -open            # live 3D viewer in the browser (blocks)
tcad watch  -R designs -open            # ...with a dropdown to pick among several
tcad shapes                             # list of primitives and parameters
tcad build model.tcad -D width=80 -D label='"Hello"'   # overrides params
```

`tcad watch` is for the user: it serves a web viewer that reloads by itself
every time the file is saved. If the user wants to see the model while you
design it together, start it in the background (for example
`tcad watch designs/part.tcad -open`) and give them the URL; every edit you make
shows up in their browser. If you work on several parts (an assembly in a
folder), pass it the folder (`-R` includes subfolders): the page has a dropdown
to pick the model, and new files show up by themselves. The viewer has a panel
with the model's `param`s (sliders when they are annotated): the user can tweak
them on the fly without editing the file, and copy the result as `-D` options.
Don't use it to check the result yourself: that's what `tcad render` and
looking at the PNG are for. The user's designs go in `designs/`, which is in `.gitignore`.

If the folder has a TCAD Studio `AGENTS.md`, you are inside the studio: the
preview is already open next to your terminal, so don't start `tcad watch`, and
follow the instructions in that file.

If `tcad` is not on the PATH, build it from the root of this repository with
`make` (or `go build -o bin/tcad ./cmd/tcad`) and use `./bin/tcad`. Views
available for `-view`: `all` (default), `iso`, `iso-back`, `iso-below`, `front`,
`back`, `left`, `right`, `top`, `bottom`.

## Workflow (always follow it)

1. **Understand the part**: what it is for, its key dimensions, how it will be
   printed and what it has to fit with. If dimensions are missing, pick
   reasonable values and expose them as `param` so the user can change them.
2. **Plan in coordinates** before writing: where each piece sits in X/Y/Z.
   X = length (left→right), Y = depth (the front is -Y), Z = height. Units: mm.
3. **Write the `.tcad`** with params at the top, derived values with `let`,
   and short comments explaining each block.
4. **Validate**: `tcad info model.tcad`. Check that the size is the expected
   one, that `bbox min` has `z = 0` (resting on the bed) and that
   `watertight: yes`.
5. **Look at the result**: `tcad render model.tcad -o /tmp/<name>.png` and open
   the image with your file or image viewing tool. Check every view: are the
   holes where they should be? Are there floating or overlapping parts? Is the
   orientation right? Use `-view` and `-size` to inspect details. Iterate
   until it's right.
6. **Generate the STL** with `tcad build` and summarize for the user the final
   dimensions, the available params and any printing considerations.

Never call a model finished without having run `tcad info` and looked at the
render at least once.

## Starting from an existing STL

When the user provides an STL to modify or reuse:

1. `tcad import part.stl -o part.tcad` (add `-center` if you want the part
   centered and resting on z = 0). The output says what was recognized
   (`box`, `cylinder`, `polygon`, `extrude`, `sphere`, `hole ...`) and how many
   bodies were left as `polyhedron`, and it checks the volume.
2. Read the generated `.tcad`. The recognized parts can be edited like any
   model: change dimensions, turn repeated values into `param`s, reorganize
   into modules. Numbers are in mm, in the original coordinate system.
3. `polyhedron`s are not parametric: don't try to edit their points by hand.
   Modify them by combining them with solids and holes inside a `group`
   (drill, trim with `hole box`, add reinforcements).
4. If the converted model is very large (many `polyhedron`s), use
   `-mode ref`, which only references the file (`stl("part.stl")`), and build
   on top of it.
5. Continue with the normal workflow: `tcad info`, `tcad render` and check the
   image.

You can also use an STL inside any model without converting it:
`stl("bracket.stl")` (centered on the workplane) or `hole stl("screw.stl", center=false)`.

## The essentials of the language

```tcad
// Comments with //, # or /* ... */
param width = 40            // configurable with -D width=60
let wall = 2                // variable (block scope)
let $sides = 64             // default resolution of round shapes

group {                                     // solids ∪ ... − holes
  box(width, 30, 20, radius=2)              // solid (the default)
  hole box(width - 2*wall, 30 - 2*wall, 20).move(z=wall)
  hole cylinder(radius=1.6, height=50).rotate(x=90).move(z=10)
}.color("blue")

for i in range(4) {                         // 0, 1, 2, 3
  cylinder(r=3, h=5).move(x=i*10 - 15, y=25)
}

module handle(length, thickness = 4) {      // reusable component
  torus(radius=length/2, tube=thickness/2)
}
handle(16).rotate(x=90).move(z=10)     // upright torus, resting on z = 0
```

Rules to keep in mind:

- **Every primitive is created centered in X and Y and resting on z = 0** (as if
  dropped on the workplane). A sphere of radius 10 goes from z=0
  to z=20; a `cylinder(height=20)` goes from z=0 to z=20. Exception: `extrude`
  and `revolve` keep the coordinates of their points.
- **Modifiers** `.move() .rotate() .scale() .mirror() .drop() .center() .color()`
  are applied **left to right and around the origin**. That's why you
  `rotate` first and `move` afterwards. `rotate(x, y, z)` is in degrees
  (applies X, then Y, then Z).
- **Grouping**: inside a `group { }` (or a `module`) the result is the union of
  all the solids minus the union of all the holes, in any order. The resulting
  group is a solid (or a hole if you write `hole group {...}`).
- **The whole file is an implicit group**: a `hole` at the top level subtracts
  from everything at the top level. To make a hole affect only one part, put
  it in that part's `group`.
- `for` and `if` do **not** create a group: their shapes go to the group that
  contains them.
- Variables can't be modified inside a loop to accumulate; compute positions
  from the index (`i * step`).
- `intersect { }` keeps only the volume its solids have in common.
- `.color()` only affects the preview; the STL has no color.

## Good modeling practices for 3D printing

- **Through holes with margin**: make them go 1 mm further on each side
  (`height = thickness + 2` and `.move(z=-1)`), so no thin skins are left.
- **Overlapping joints**: when two solids must be joined, overlap them
  slightly (0.5–1 mm) instead of letting them touch only at an edge or a
  point, which produces non-manifold geometry.
- **Resting on the bed**: the part should have its largest flat face at
  z = 0. After rotating, use `.drop()` to bring it down to the workplane.
- **Walls ≥ 1.2 mm** (2 mm is better), **clearances** of 0.2–0.4 mm per side in
  fits, overhangs ≤ 45°, or use chamfers instead of flat ceilings.
- **Metric fasteners** (through holes): M2 ⌀2.4, M3 ⌀3.4, M4 ⌀4.5,
  M5 ⌀5.5. Countersinks/heads: see patterns.md.
- **Text**: size ≥ 5 mm, raised or engraved 0.6–1.5 mm, preferably
  `font="sans-bold"`.
- **Resolution**: `let $sides = 64` for visible parts; 32 is enough for
  prototypes. Very high values (>128) or spheres with high `steps` make the CSG
  slow. 32 is enough for small screws.
- Expose as `param` the dimensions the user will probably want to adjust,
  with a range annotation and a description for the `tcad watch` panel:
  `param wall = 2 // [1.2:0.2:4] Wall thickness (mm)`. Choose ranges that
  produce valid parts (walls that don't vanish, reasonable clearances).
- If a parameter only has a few possible values (modes such as
  "assembled"/"print", fonts, metric sizes), declare them with `in`:
  `param mode = "assembled" in ["assembled", "print"]`. Don't leave it as free
  text explained in a comment: with `in` it is validated, and a typo in a
  comparison is caught.

## Common mistakes

| Symptom | Cause and fix |
|---|---|
| A hole doesn't cut anything | It is outside the part's `group`, or doesn't touch it. Check with the render. |
| A hole cuts parts it shouldn't | It is at the top level: put it in the right `group`. |
| The part is offset after rotating | It was moved before rotating. Use `.rotate(...).move(...)`. |
| `bbox min` with z < 0 | Something is below the bed. Adjust positions or add `.drop()` to the final group. |
| `watertight: no` | Contacts only at an edge/point, or near-exact coincidences. Add 0.5 mm overlaps. |
| Faceted shapes | Raise `$sides` (or `sides=` on the primitive). |
| `unknown shape`, `has no parameter` | The message suggests the right name; check `tcad shapes`. |
