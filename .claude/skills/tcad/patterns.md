# Recetas de diseño con TCAD

Cada bloque es un modelo completo que funciona tal cual. Combínalos y adapta
las medidas.

## Caja hueca (vaciado con paredes)

```tcad
param largo = 60
param ancho = 40
param alto = 25
param pared = 2

group {
  box(largo, ancho, alto, radius=2)
  // El interior empieza en z = pared (suelo) y sobresale por arriba: caja abierta.
  hole box(largo - 2*pared, ancho - 2*pared, alto, radius=1).move(z=pared)
}
```

Para una carcasa cerrada, el hueco mide `alto - 2*pared` y se sube `pared`.

## Tapa que encaja por dentro

```tcad
param largo = 60
param ancho = 40
param pared = 2
param holgura = 0.3     // por lado; 0.2 ajustado, 0.4 holgado

let int_l = largo - 2*pared
let int_a = ancho - 2*pared

group {
  box(largo, ancho, pared, radius=1)                          // placa
  box(int_l - 2*holgura, int_a - 2*holgura, pared + 4)        // pestaña
  hole box(int_l - 2*holgura - 2*pared, int_a - 2*holgura - 2*pared, 10).move(z=pared)
}
```

## Agujeros para tornillos y tuercas

```tcad
let $sides = 32
let grosor = 6

// Diámetros pasantes recomendados
func paso(m) = m == 2 ? 2.4 : m == 3 ? 3.4 : m == 4 ? 4.5 : 5.5

group {
  box(60, 20, grosor)

  // 1) Pasante simple M3
  hole cylinder(radius=paso(3)/2, height=grosor + 2).move(x=-20, z=-1)

  // 2) Avellanado M3 (cabeza a 90°): cono invertido en la cara superior
  hole cylinder(radius=paso(3)/2, height=grosor + 2).move(z=-1)
  hole cone(base_radius=paso(3)/2, top_radius=3.2, height=1.8).move(z=grosor - 1.8 + 0.01)

  // 3) Alojamiento de tuerca M3 por debajo (5.5 entre caras + holgura)
  hole cylinder(radius=paso(3)/2, height=grosor + 2).move(x=20, z=-1)
  hole polygon(sides=6, radius=(5.5 + 0.4) / 2 / cos(30), height=2.5 + 1).move(x=20, z=-1)
}
```

- Tuerca hexagonal: `polygon(sides=6, radius = entre_caras / 2 / cos(30))`.
- Cabeza cilíndrica (allen M3): `cylinder(radius=3.2, height=3.2)` en la cara.
- Agujero para inserto térmico M3: ⌀4.0–4.2, profundidad 5–6 mm.

## Ranura (agujero alargado)

No existe `hull`; una ranura es una caja más dos cilindros en los extremos.

```tcad
module ranura(largo, ancho, alto) {
  box(largo - ancho, ancho, alto)
  cylinder(radius=ancho/2, height=alto).move(x=-(largo - ancho)/2)
  cylinder(radius=ancho/2, height=alto).move(x=(largo - ancho)/2)
}

group {
  box(50, 20, 4, radius=1)
  hole ranura(30, 4, 10).move(z=-1)
}
```

## Repeticiones: rejilla y matriz polar

```tcad
param filas = 3
param columnas = 4
let paso = 12

group {
  box(columnas * paso + 6, filas * paso + 6, 3, radius=2)
  for i in range(columnas) {
    for j in range(filas) {
      hole cylinder(r=4, h=5).move(x=(i - (columnas - 1)/2) * paso,
                                   y=(j - (filas - 1)/2) * paso, z=-1)
    }
  }
}

// Matriz polar: rotar alrededor del eje Z después de desplazar desde el centro
for k in range(8) {
  box(4, 10, 6).move(y=30).rotate(z=k * 360 / 8)
}
```

## Girar alrededor de un punto que no es el origen

Lleva el punto al origen, gira y devuélvelo:

```tcad
let px = 20
let pz = 5
box(40, 4, 10)
  .move(x=-px, z=-pz)
  .rotate(y=-30)
  .move(x=px, z=pz)
```

## Redondeos y chaflanes

```tcad
// Redondeo de todas las aristas
box(30, 20, 10, radius=2, steps=8)

// Cilindro con bordes redondeados (segments > 1) o achaflanados (segments = 1)
cylinder(radius=8, height=10, bevel=1.5, segments=6).move(x=30)
cylinder(radius=8, height=10, bevel=1.5, segments=1).move(x=50)

// Chaflán en una sola arista: restar un tejado girado 45°
group {
  box(30, 20, 10)
  hole roof(32, 4, 2).move(z=-2).rotate(x=45).move(y=-10, z=10)
}.move(y=30)
```

## Texto en relieve, grabado y en caras verticales

```tcad
group {
  box(60, 25, 4, radius=1)
  // Grabado de 1 mm en la cara superior: el hueco sobresale por arriba
  hole text("HOLA", size=8, height=2, font="sans-bold").move(z=3)
}

// Relieve: segunda placa con el texto sobre su cara superior (z = 4). El texto
// se hunde 0.5 mm en la placa para que la unión sea sólida: sobresale 1 mm.
box(60, 25, 4, radius=1).move(y=-35)
text("TCAD", size=8, height=1.5, font="sans-bold").move(y=-35, z=3.5)

// Texto en la cara frontal (-Y) de una caja: girar 90° en X lo pone de pie
// mirando hacia -Y (de 0 a -height en Y), luego se lleva a la cara.
group {
  box(60, 20, 20).move(y=40)
  hole text("FRENTE", size=6, height=1.5).rotate(x=90).move(y=40 - 10 + 0.8, z=10)
}
```

## Perfiles a medida: extrude y revolve

```tcad
// Escuadra en L de 3 mm de grosor y 15 de ancho: perfil en XY, extruido en Z
// y tumbada después para imprimir apoyada.
extrude(points=[[0, 0], [40, 0], [40, 3], [3, 3], [3, 30], [0, 30]], height=15)

// Vaso: perfil [radio, z] girado alrededor de Z
group {
  revolve(points=[[0, 0], [25, 0], [32, 80], [0, 80]], sides=64)
  hole revolve(points=[[0, 2], [23, 2], [30, 81], [0, 81]], sides=64)
}.move(x=80)
```

Para generar listas de puntos de forma programática, usa una función
recursiva que concatene listas:

```tcad
// Polígono de n lados con un radio que oscila (flor)
func petalo(i, n, r) = r + 3 * cos(i * 360 / n * 6)
func flor(i, n, r) = i >= n ? [] :
  [[petalo(i, n, r) * cos(i * 360 / n), petalo(i, n, r) * sin(i * 360 / n)]] + flor(i + 1, n, r)

extrude(points=flor(0, 120, 20), height=5)
```

(La recursión admite hasta ~250 niveles; para más puntos, divide en tramos.)

## Piezas multi-parte en la misma cama

Coloca cada parte en su propio `group` y sepáralas con `.move()` al menos 5–10
mm. Cada grupo mantiene sus propios huecos.

```tcad
group { box(30, 30, 10)  hole box(26, 26, 10).move(z=2) }
group { box(30, 30, 2)   box(25.6, 25.6, 4) }.move(x=40)
```

## Intersección para recortar

```tcad
// Esfera recortada para tener una base plana de apoyo
intersect {
  sphere(radius=15).move(z=-5)
  box(40, 40, 30)
}
```

## Comprobaciones antes de entregar

1. `tcad info`: medidas esperadas, `bbox min z = 0`, `watertight: yes`.
2. `tcad render` y revisar las cuatro vistas (y `-view iso-below` si importa la base).
3. Paredes ≥ 1.2 mm, holguras de encaje 0.2–0.4 mm, voladizos ≤ 45°.
4. `tcad build` y entregar el STL junto con la lista de `param` ajustables.
