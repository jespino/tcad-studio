# TCAD design recipes

Every block is a complete model that works as is. Combine them and adapt the
dimensions.

## Hollow box (shelled with walls)

```tcad
param length = 60
param width = 40
param height = 25
param wall = 2

group {
  box(length, width, height, radius=2)
  // The inside starts at z = wall (the floor) and sticks out at the top: an open box.
  hole box(length - 2*wall, width - 2*wall, height, radius=1).move(z=wall)
}
```

For a closed shell, the hole is `height - 2*wall` tall and raised by `wall`.

## Lid that fits inside

```tcad
param length = 60
param width = 40
param wall = 2
param clearance = 0.3   // per side; 0.2 is tight, 0.4 is loose

let in_l = length - 2*wall
let in_w = width - 2*wall

group {
  box(length, width, wall, radius=1)                            // plate
  box(in_l - 2*clearance, in_w - 2*clearance, wall + 4)         // lip
  hole box(in_l - 2*clearance - 2*wall, in_w - 2*clearance - 2*wall, 10).move(z=wall)
}
```

## Holes for screws and nuts

```tcad
let $sides = 32
let thickness = 6

// Recommended through-hole diameters
func clear(m) = m == 2 ? 2.4 : m == 3 ? 3.4 : m == 4 ? 4.5 : 5.5

group {
  box(60, 20, thickness)

  // 1) Plain M3 through hole
  hole cylinder(radius=clear(3)/2, height=thickness + 2).move(x=-20, z=-1)

  // 2) M3 countersink (90° head): an inverted cone on the top face
  hole cylinder(radius=clear(3)/2, height=thickness + 2).move(z=-1)
  hole cone(base_radius=clear(3)/2, top_radius=3.2, height=1.8).move(z=thickness - 1.8 + 0.01)

  // 3) M3 nut trap from below (5.5 across flats + clearance)
  hole cylinder(radius=clear(3)/2, height=thickness + 2).move(x=20, z=-1)
  hole polygon(sides=6, radius=(5.5 + 0.4) / 2 / cos(30), height=2.5 + 1).move(x=20, z=-1)
}
```

- Hex nut: `polygon(sides=6, radius = across_flats / 2 / cos(30))`.
- Socket head (M3 Allen): `cylinder(radius=3.2, height=3.2)` on the face.
- Hole for an M3 heat-set insert: ⌀4.0–4.2, 5–6 mm deep.

## Slot (elongated hole)

There is no `hull`; a slot is a box plus two cylinders at the ends.

```tcad
module slot(length, width, height) {
  box(length - width, width, height)
  cylinder(radius=width/2, height=height).move(x=-(length - width)/2)
  cylinder(radius=width/2, height=height).move(x=(length - width)/2)
}

group {
  box(50, 20, 4, radius=1)
  hole slot(30, 4, 10).move(z=-1)
}
```

## Repetitions: grid and polar array

```tcad
param rows = 3
param columns = 4
let step = 12

group {
  box(columns * step + 6, rows * step + 6, 3, radius=2)
  for i in range(columns) {
    for j in range(rows) {
      hole cylinder(r=4, h=5).move(x=(i - (columns - 1)/2) * step,
                                   y=(j - (rows - 1)/2) * step, z=-1)
    }
  }
}

// Polar array: rotate around the Z axis after moving away from the center
for k in range(8) {
  box(4, 10, 6).move(y=30).rotate(z=k * 360 / 8)
}
```

## Rotating around a point other than the origin

Move the point to the origin, rotate, and move it back:

```tcad
let px = 20
let pz = 5
box(40, 4, 10)
  .move(x=-px, z=-pz)
  .rotate(y=-30)
  .move(x=px, z=pz)
```

## Roundings and chamfers

```tcad
// Rounding every edge
box(30, 20, 10, radius=2, steps=8)

// Cylinder with rounded (segments > 1) or chamfered (segments = 1) edges
cylinder(radius=8, height=10, bevel=1.5, segments=6).move(x=30)
cylinder(radius=8, height=10, bevel=1.5, segments=1).move(x=50)

// Chamfer on a single edge: subtract a roof rotated 45°
group {
  box(30, 20, 10)
  hole roof(32, 4, 2).move(z=-2).rotate(x=45).move(y=-10, z=10)
}.move(y=30)
```

## Raised, engraved and vertical-face text

```tcad
group {
  box(60, 25, 4, radius=1)
  // 1 mm engraving on the top face: the hole sticks out at the top
  hole text("HELLO", size=8, height=2, font="sans-bold").move(z=3)
}

// Raised: a second plate with the text on its top face (z = 4). The text
// sinks 0.5 mm into the plate for a solid joint: it sticks out 1 mm.
box(60, 25, 4, radius=1).move(y=-35)
text("TCAD", size=8, height=1.5, font="sans-bold").move(y=-35, z=3.5)

// Text on the front face (-Y) of a box: rotating 90° about X stands it up
// facing -Y (from 0 to -height in Y), then it is moved to the face.
group {
  box(60, 20, 20).move(y=40)
  hole text("FRONT", size=6, height=1.5).rotate(x=90).move(y=40 - 10 + 0.8, z=10)
}
```

## Custom profiles: extrude and revolve

```tcad
// L bracket 3 mm thick and 15 wide: profile in XY, extruded along Z,
// and laid down afterwards to print flat.
extrude(points=[[0, 0], [40, 0], [40, 3], [3, 3], [3, 30], [0, 30]], height=15)

// Cup: a [radius, z] profile revolved around Z
group {
  revolve(points=[[0, 0], [25, 0], [32, 80], [0, 80]], sides=64)
  hole revolve(points=[[0, 2], [23, 2], [30, 81], [0, 81]], sides=64)
}.move(x=80)
```

To generate lists of points programmatically, use a recursive function that
concatenates lists:

```tcad
// n-sided polygon with an oscillating radius (a flower)
func petal(i, n, r) = r + 3 * cos(i * 360 / n * 6)
func flower(i, n, r) = i >= n ? [] :
  [[petal(i, n, r) * cos(i * 360 / n), petal(i, n, r) * sin(i * 360 / n)]] + flower(i + 1, n, r)

extrude(points=flower(0, 120, 20), height=5)
```

(Recursion allows up to ~250 levels; for more points, split it into chunks.)

## Multi-part prints on the same bed

Put each part in its own `group` and keep them at least 5–10 mm apart with
`.move()`. Each group keeps its own holes.

```tcad
group { box(30, 30, 10)  hole box(26, 26, 10).move(z=2) }
group { box(30, 30, 2)   box(25.6, 25.6, 4) }.move(x=40)
```

## Intersection to trim

```tcad
// A sphere trimmed to get a flat base to rest on
intersect {
  sphere(radius=15).move(z=-5)
  box(40, 40, 30)
}
```

## Checks before delivering

1. `tcad info`: expected dimensions, `bbox min z = 0`, `watertight: yes`.
2. `tcad render` and check the four views (and `-view iso-below` if the base matters).
3. Walls ≥ 1.2 mm, fit clearances 0.2–0.4 mm, overhangs ≤ 45°.
4. `tcad build` and deliver the STL together with the list of adjustable `param`s.
