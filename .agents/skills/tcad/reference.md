# TCAD language reference

## Coordinate system

- Units: millimeters. Angles: degrees.
- X = length (left → right), Y = depth (the model's **front** is -Y; the
  `front` view looks from -Y), Z = height. The workplane / bed is z = 0.
- Every primitive is created **centered in X and Y, with its lowest point at
  z = 0**. Example: `box(20, 10, 5)` takes x∈[-10,10], y∈[-5,5], z∈[0,5].
  `extrude` and `revolve` are the exception: they keep the given coordinates.

## File structure

A file is a list of statements. The whole file behaves like a `group`: the
final STL is the union of the top-level solids minus the top-level holes.
Semicolons at the end of a line are optional.

Comments: `// line`, `# line`, `/* block */`.

### Statements

| Statement | Description |
|---|---|
| `param name = expr` | Configurable parameter (`tcad ... -D name=value` or from the `tcad watch` panel). Top level only. The new value must have the same type. Accepts an annotation in a comment on the same line (see below). |
| `param name = expr in [v1, v2, ...]` | Parameter with a **closed set of values**. The default, the `-D` values and the changes made in the panel must be one of them (otherwise, an error lists them). Comparing the parameter with a literal that is not in the list (`mode == "printing"`) is an error. The panel shows it as a dropdown. |
| `let name = expr` | Variable. Visible in its block and in nested blocks. Redefining it in an inner block hides it only inside that block. |
| `func name(a, b = 1) = expr` | Pure single-expression function. Top level only; it can be used before it is defined; recursion is allowed. |
| `module name(a, b = 1) { ... }` | Reusable component. It is used like a shape: `name(3, b=2)`. Its body is a group. Top level only. |
| `[solid\|hole] shape .mod(...) .mod(...)` | Places a shape. `solid` is the default. |
| `for x in list { ... }` | Repeats the body; the shapes are added to the group that contains the loop. |
| `if cond { ... } else if cond { ... } else { ... }` | Conditional; like `for`, it doesn't create a group. |
| `echo(expr, name = expr, ...)` | Prints values to stderr for debugging. |

Inside a `module` you only see its parameters, its `let`s and the global
variables (the top-level `let`s/`param`s defined **before** the call). It
doesn't see the caller's local variables: pass them as arguments.

### Allowed values and `param` annotations

When a parameter can only take a few specific values (a mode, a font, a screw
size), declare them with `in`. It is part of the language and it is checked:

```tcad
param mode   = "assembled" in ["assembled", "print"]   // View or separate parts
param metric = 3 in [2, 2.5, 3, 4, 5]                  // Screw size M
param font   = "sans" in ["sans", "mono"]
```

The comment at the end of the line is an **annotation** for the `tcad watch`
panel (like the OpenSCAD Customizer): an optional range for the slider and a
description. Ranges are only a hint for the interface; they don't limit the
value when compiling.

```tcad
param width  = 60      // [20:200] Outer width (mm)        → slider from 20 to 200
param wall   = 2       // [1.2:0.2:4] Wall thickness       → [min:step:max]
param lid    = true    // Also print the lid               → checkbox + description
```

Without a range, numbers are edited in a number field; `bool`s with a
checkbox; parameters with `in` with a dropdown.

### Shapes

- `primitive(args)` — see the table of primitives.
- `module_name(args)` — instantiates a module.
- `group { statements }` — result = ∪ solids − ∪ holes (order doesn't matter).
- `intersect { statements }` — result = ∩ solids − ∪ holes.

`hole` prefix: the resulting shape is a hole in the group that contains it.
`hole group { ... }` turns a whole group into a hole (useful for compound
holes). A group without solids produces nothing.

### Arguments

Positional in the order of the table and/or named: `box(20, 10, height=5)`.
The primitives with `length`, `width`, `height` also accept
`size=[l, w, h]`. The aliases (`r`, `h`, `l`, `w`...) are equivalent to the
long name.

### Modifiers

They are chained after the shape and applied **in order and around the
origin**.

| Modifier | Effect |
|---|---|
| `.move(x, y, z)` / `.translate(...)` | Translates. Accepts names: `.move(z=5)`. |
| `.rotate(x, y, z)` | Rotates in degrees around the world axes: first X, then Y, then Z. To rotate around the part's center, rotate it before moving it. |
| `.scale(s)` / `.scale(x, y, z)` | Uniform or per-axis scale (from the origin). |
| `.mirror("x")` | Mirrors along the given axis (`"x"`, `"y"`, `"z"` or combinations such as `"xy"`). Also `.mirror(x=true)`. |
| `.drop()` | Moves in Z so the lowest point is at z = 0. |
| `.center("xy")` | Centers the bounding box on the given axes (`"xy"` by default; `"xyz"` also centers in Z). |
| `.color(c)` | Preview color: a name (`red orange yellow green blue purple pink white black gray brown cyan magenta teal navy lime`), `"#rrggbb"` or `r, g, b` (0–255). Faces cut by a hole take the color of the group's first solid. |

### Curve quality

`let $sides = N` sets the default of the `sides` parameter (segments around a
circle) and `let $steps = N` the default of `steps` (segments of curved
profiles: spheres, roundings...). They are scoped: a `let $sides` inside a
group or module only affects that block. An explicit argument (`sides=8`)
always wins.

## Expressions

- Types: number (`3`, `2.5`, `1e-3`), string (`"text"`, escapes `\n \t \" \\`),
  boolean (`true`, `false`), list (`[1, 2, [3, 4]]`).
- Operators, from lowest to highest precedence:
  `?:` · `||` · `&&` · `== !=` · `< <= > >=` · `+ -` · `* / %` · unary `- + !` · index `x[i]`.
- `+` concatenates strings (`"M" + 3` → `"M3"`) and lists (`[1] + [2]`).
- Negative indexes count from the end (`list[-1]`).
- Truthiness: `false`, `0`, `""` and `[]` are false.
- Constant: `PI`.

### Built-in functions

| Function | Description |
|---|---|
| `sin cos tan` | Trigonometry in **degrees**. |
| `asin acos atan atan2(y, x)` | Inverses, they return degrees. |
| `sqrt abs floor ceil round sign exp ln pow(b, e)` | Math. |
| `min(a, b, ...)`, `max(...)` | They also accept a list: `max(list)`. |
| `len(x)` | Length of a list or a string. |
| `str(a, b, ...)` | Concatenates values as text. |
| `range(n)`, `range(a, b)`, `range(a, b, step)` | List `[a, a+step, ...]` **not including** `b`. `range(3)` → `[0, 1, 2]`. |

## Primitives

Defaults in parentheses. `sides`/`steps` marked with * take `$sides`/`$steps`
when they are defined.

| Primitive | Parameters (positional order) | Notes |
|---|---|---|
| `box` | `length`(20) `width`(20) `height`(20) `radius`(0) `steps`*(8) | `radius` rounds every edge; `steps` = segments of the rounding. |
| `cylinder` | `radius`(10) `height`(20) `sides`*(32) `bevel`(0) `segments`(1) | `sides=6` gives a hexagonal prism. `bevel` bevels the top and bottom edges; `segments=1` is a chamfer, >1 a rounding. |
| `cone` | `base_radius`(10) `top_radius`(0) `height`(20) `sides`*(32) | With `top_radius > 0` it is a truncated cone. Aliases: `radius`/`r`/`r1`, `r2`. |
| `sphere` | `radius`(10) `steps`*(16) | Resting on the workplane: center at z = radius. |
| `half_sphere` | `radius`(10) `steps`*(16) | Dome; flat face at the bottom. |
| `torus` | `radius`(10) `tube`(2) `sides`*(32) `steps`*(16) | `radius` = from the center to the tube's axis; total size 2·(radius+tube). `tube < radius`. |
| `tube` | `radius`(10) `wall`(2) `height`(20) `sides`*(32) `bevel`(0) `segments`(1) | Hollow cylinder; `radius` is the outer one. Alias `thickness` = `wall`. |
| `paraboloid` | `radius`(10) `height`(20) `sides`*(32) `steps`*(16) | Base at the bottom, tip at the top. |
| `wedge` | `length`(20) `width`(20) `height`(20) | Wedge: triangular profile in XZ extruded along Y. Vertical face at -X, slopes down towards +X. |
| `roof` | `length`(20) `width`(20) `height`(20) | Roof: triangular prism, the ridge runs along X. |
| `round_roof` | `length`(20) `width`(20) `height`(10) `sides`*(32) | Half cylinder lying along X. |
| `pyramid` | `length`(20) `width`(20) `height`(20) `sides`(4) | With 4 sides, a rectangular L×W base; with other values, a regular polygon. |
| `polygon` | `sides`(6) `radius`(10) `height`(20) | Regular polygon prism (radius to the vertices). |
| `star` | `points`(5) `radius`(10) `inner_radius`(5) `height`(5) | Star with one point towards +Y. |
| `heart` | `length`(20) `width`(20) `height`(5) | Heart fitted to L×W. |
| `icosahedron` | `radius`(10) | Radius to the vertices. |
| `text` | `text`("Text") `size`(10) `height`(3) `font`("sans") `spacing`(0) `align`("center") | `size` = height of the capital letters in mm. Fonts: `sans`, `sans-bold`, `sans-italic`, `sans-bold-italic`, `mono`, `mono-bold`. `\n` for several lines. The text reads from above (+Z); centered in XY. |
| `extrude` | `points`(required) `height`(10) | 2D polygon `[[x, y], ...]` (simple, without self-intersections, any winding) extruded from z=0 to `height`. **Keeps the coordinates.** |
| `revolve` | `points`(required) `sides`*(32) | Profile `[[r, z], ...]` with r ≥ 0 revolved around the Z axis. The profile closes by itself; points with r = 0 lie on the axis. **Keeps the coordinates.** |
| `stl` | `file`(required) `center`(true) | Imports an STL (binary or ASCII). Path relative to the `.tcad`. With `center=true` it is centered in XY and rests on z = 0; with `false` it keeps its coordinates. It can be used as a solid or a `hole`, and with modifiers. |
| `polyhedron` | `points`(required) `faces`(required) | Arbitrary mesh: `points=[[x, y, z], ...]`, `faces=[[i, j, k], ...]` (indexes from 0, faces counterclockwise seen from outside; polygons are allowed). It must be closed. **Keeps the coordinates.** It is what `tcad import` generates for bodies it can't recognize. |

## Grouping semantics, with examples

```tcad
// 1) A hole inside the group: it only affects that group
group {
  box(30, 30, 10)
  hole cylinder(r=5, h=12).move(z=-1)
}
box(10, 10, 10).move(x=40)        // not affected by the hole above

// 2) A hole at the top level: it subtracts from the whole top level
box(30, 30, 10)
box(10, 10, 10).move(x=20)
hole box(100, 4, 100).move(z=-1)  // cuts both boxes

// 3) Compound hole: grouped and used as a single hole
group {
  box(40, 40, 10)
  hole group {
    cylinder(r=1.7, h=12)              // shank of an M3 screw
    cylinder(r=3.2, h=4).move(z=8)     // pocket for the head
  }.move(z=-1)                         // z from -1 to 11: goes through the box
}

// 4) A module's holes only hollow out the module itself
module washer(d_out, d_in, h) {
  cylinder(radius=d_out/2, height=h)
  hole cylinder(radius=d_in/2, height=h + 2).move(z=-1)
}
washer(20, 8, 2)
```

## Error messages

Errors give `file:line:column` and, if a name is misspelled, suggest the right
one (`did you mean "cylinder"?`). Typical errors: `unknown shape`,
`has no parameter`, `undefined variable`, `expects a number`,
`missing required parameter`, `the model is empty`.
