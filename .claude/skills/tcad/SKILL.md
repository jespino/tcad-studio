---
name: tcad
description: Diseña modelos 3D imprimibles con el lenguaje TCAD (primitivas al estilo Tinkercad como box, cylinder, sphere, cone, torus, text... combinadas como sólidos y huecos) genera STL y previsualizaciones PNG, e importa STL existentes para convertirlos en TCAD, con la herramienta `tcad`. Úsalo cuando el usuario pida crear, modificar, revisar, convertir o explicar un modelo 3D, una pieza para imprimir en 3D, un fichero .tcad o un STL.
---

# TCAD: modelado 3D por sólidos y huecos

TCAD es un lenguaje declarativo para describir piezas 3D igual que en Tinkercad:
colocas primitivas (cajas, cilindros, esferas...), marcas algunas como **huecos**
(`hole`) y al **agrupar** (`group { }`) los huecos se restan de los sólidos.
La herramienta `tcad` convierte el fichero en STL (para imprimir) o en PNG
(para revisar el resultado).

- Referencia completa del lenguaje y de cada primitiva: [reference.md](reference.md)
- Recetas de diseño (cajas, tapas, tornillos, matrices polares, texto...): [patterns.md](patterns.md)

Lee `reference.md` la primera vez que escribas un modelo en una conversación y
`patterns.md` cuando la pieza tenga encajes, tornillería, repeticiones o texto.

## La herramienta

```bash
tcad check  modelo.tcad                 # solo valida (errores con línea:columna)
tcad info   modelo.tcad                 # tamaño, bbox, volumen, ¿estanco?, params
tcad render modelo.tcad -o vista.png    # hoja 2x2: iso, frontal, derecha, planta
tcad render modelo.tcad -view iso-below -size 700 -o abajo.png
tcad build  modelo.tcad -o pieza.stl    # STL binario (-ascii para texto)
tcad import pieza.stl -o pieza.tcad     # convierte un STL en TCAD
tcad watch  modelo.tcad -open           # visor 3D en vivo en el navegador (bloquea)
tcad watch  -R diseños -open            # ...con un desplegable para elegir entre varios
tcad shapes                             # lista de primitivas y parámetros
tcad build modelo.tcad -D ancho=80 -D texto='"Hola"'   # sobrescribe params
```

`tcad watch` es para el usuario: sirve un visor web que se recarga solo cada
vez que se guarda el fichero. Si el usuario quiere ver el modelo mientras lo
diseñáis juntos, arráncalo en segundo plano (por ejemplo
`tcad watch diseños/pieza.tcad -open`) y dile la URL; cada edición que hagas
aparecerá en su navegador. Si la carpeta tiene un `AGENTS.md` de TCAD Studio,
estás dentro del estudio: la vista previa ya está abierta junto a tu terminal,
no lances `tcad watch` y sigue las instrucciones de ese fichero. Si trabajáis con varias piezas (un conjunto en un
directorio), pásale el directorio (`-R` incluye subdirectorios): la página
tiene un desplegable para elegir el modelo y los ficheros nuevos aparecen solos. El visor tiene un panel con los `param` del modelo
(deslizadores si están anotados): el usuario puede ajustarlos al vuelo sin
editar el fichero y copiar el resultado como opciones `-D`. No lo uses para comprobar tú el resultado: para eso
está `tcad render` + Read. Los diseños del usuario van en `diseños/`, que está
en `.gitignore`.

Si `tcad` no está en el PATH, compílalo desde la raíz de este repositorio con
`make` (o `go build -o bin/tcad ./cmd/tcad`) y usa `./bin/tcad`. Vistas disponibles para
`-view`: `all` (por defecto), `iso`, `iso-back`, `iso-below`, `front`, `back`,
`left`, `right`, `top`, `bottom`.

## Flujo de trabajo (síguelo siempre)

1. **Entiende la pieza**: función, medidas clave, cómo se imprimirá y qué
   tiene que encajar con qué. Si faltan medidas, elige valores razonables y
   exponlos como `param` para que el usuario pueda cambiarlos.
2. **Planifica en coordenadas** antes de escribir: dónde cae cada pieza en X/Y/Z.
   X = largo (izquierda→derecha), Y = fondo (el frente es -Y), Z = alto. Unidades: mm.
3. **Escribe el `.tcad`** con params arriba, variables derivadas con `let`,
   y comentarios breves que expliquen cada bloque.
4. **Valida**: `tcad info modelo.tcad`. Comprueba que el tamaño es el esperado,
   que `bbox min` tiene `z = 0` (apoyada en la cama) y que `watertight: yes`.
5. **Mira el resultado**: `tcad render modelo.tcad -o /tmp/<nombre>.png` y abre
   la imagen con la herramienta Read. Revisa cada vista: ¿están los huecos donde
   deben?, ¿hay piezas flotando o atravesando?, ¿la orientación es correcta?
   Usa `-view` y `-size` para inspeccionar detalles. Itera hasta que esté bien.
6. **Genera el STL** con `tcad build` y resume al usuario las medidas finales,
   los params disponibles y cualquier consideración de impresión.

Nunca des un modelo por terminado sin haber ejecutado `tcad info` y haber
mirado al menos una vez el render.

## Partir de un STL existente

Cuando el usuario aporte un STL para modificarlo o reutilizarlo:

1. `tcad import pieza.stl -o pieza.tcad` (añade `-center` si quieres la pieza
   centrada y apoyada en z = 0). La salida indica qué se reconoció
   (`box`, `cylinder`, `polygon`, `extrude`, `sphere`, `hole ...`) y cuántos
   cuerpos quedaron como `polyhedron`, y verifica el volumen.
2. Lee el `.tcad` generado. Las piezas reconocidas se pueden editar como
   cualquier modelo: cambiar medidas, convertir valores repetidos en `param`,
   reorganizar en módulos. Las cifras salen en mm con el sistema de
   coordenadas original.
3. Los `polyhedron` no son paramétricos: no intentes editar sus puntos a mano.
   Modifícalos combinándolos con sólidos y huecos dentro de un `group`
   (taladrar, recortar con `hole box`, añadir refuerzos).
4. Si el modelo convertido es muy grande (muchos `polyhedron`), usa
   `-mode ref`, que solo referencia el fichero (`stl("pieza.stl")`), y trabaja
   encima de él.
5. Sigue con el flujo normal: `tcad info`, `tcad render` y revisar la imagen.

También puedes usar un STL dentro de cualquier modelo sin convertirlo:
`stl("soporte.stl")` (centrado en el plano) o `hole stl("tornillo.stl", center=false)`.

## Lo esencial del lenguaje

```tcad
// Comentarios con //, # o /* ... */
param ancho = 40            // configurable con -D ancho=60
let pared = 2               // variable (ámbito de bloque)
let $sides = 64             // resolución por defecto de lo circular

group {                                     // sólidos ∪ ... − huecos
  box(ancho, 30, 20, radius=2)              // sólido (por defecto)
  hole box(ancho - 2*pared, 30 - 2*pared, 20).move(z=pared)
  hole cylinder(radius=1.6, height=50).rotate(x=90).move(z=10)
}.color("blue")

for i in range(4) {                         // 0, 1, 2, 3
  cylinder(r=3, h=5).move(x=i*10 - 15, y=25)
}

module asa(largo, grosor = 4) {             // componente reutilizable
  torus(radius=largo/2, tube=grosor/2)
}
asa(16).rotate(x=90).move(z=10)        // torus de pie, apoyado en z = 0
```

Reglas que hay que tener claras:

- **Toda primitiva nace centrada en X e Y y apoyada en z = 0** (como al soltarla
  en el plano de trabajo de Tinkercad). Una esfera de radio 10 va de z=0 a z=20;
  un `cylinder(height=20)` va de z=0 a z=20. Excepción: `extrude` y `revolve`
  conservan las coordenadas de sus puntos.
- **Modificadores** `.move() .rotate() .scale() .mirror() .drop() .center() .color()`
  se aplican **de izquierda a derecha y alrededor del origen**. Por eso:
  primero `rotate`, después `move`. `rotate(x, y, z)` en grados (aplica X, luego Y, luego Z).
- **Agrupación**: dentro de un `group { }` (o un `module`) el resultado es
  la unión de todos los sólidos menos la unión de todos los huecos, sin importar
  el orden. El grupo resultante es sólido (o hueco si se escribe `hole group {...}`).
- **El fichero entero es un grupo implícito**: un `hole` en el nivel superior
  resta de todo lo del nivel superior. Para que un hueco afecte solo a una pieza,
  mételo en el `group` de esa pieza.
- `for` e `if` **no** crean grupo: sus formas pasan al grupo que los contiene.
- Las variables no se pueden modificar dentro de un bucle para acumular; calcula
  posiciones a partir del índice (`i * paso`).
- `intersect { }` deja solo el volumen común de sus sólidos.
- `.color()` solo afecta a la previsualización; el STL no tiene color.

## Buenas prácticas de modelado para impresión 3D

- **Huecos pasantes con margen**: haz que atraviesen 1 mm más por cada lado
  (`height = grosor + 2` y `.move(z=-1)`), así no quedan pieles finas.
- **Uniones con solape**: cuando dos sólidos deben quedar unidos, solápalos
  ligeramente (0.5–1 mm) en vez de dejar que solo se toquen en una arista o un
  punto, que produce geometría no manifold.
- **Apoyo en la cama**: la pieza debe tener su cara plana más grande en z = 0.
  Tras rotar, usa `.drop()` para bajarla al plano.
- **Paredes ≥ 1.2 mm** (mejor 2 mm), **holguras** de 0.2–0.4 mm por lado en
  encajes, voladizos ≤ 45° o usar chaflanes en vez de techos planos.
- **Tornillería métrica** (agujeros pasantes): M2 ⌀2.4, M3 ⌀3.4, M4 ⌀4.5,
  M5 ⌀5.5. Avellanados/cabezas: ver patterns.md.
- **Texto**: tamaño ≥ 5 mm, relieve o grabado de 0.6–1.5 mm, preferiblemente
  `font="sans-bold"`.
- **Resolución**: `let $sides = 64` para piezas visibles; 32 basta para
  prototipos. Valores muy altos (>128) o esferas con `steps` altos hacen el CSG
  lento. En tornillos pequeños 32 es suficiente.
- Expón como `param` las medidas que el usuario probablemente quiera ajustar,
  con una anotación de rango y descripción para el panel de `tcad watch`:
  `param pared = 2 // [1.2:0.2:4] Grosor de pared (mm)`. Elige rangos que
  produzcan piezas válidas (paredes que no se anulen, holguras razonables).
- Si un parámetro solo tiene unos valores posibles (modos como
  "montado"/"impresion", fuentes, métricas), decláralos con `in`:
  `param modo = "montado" in ["montado", "impresion"]`. No lo dejes como texto
  libre explicado en un comentario: con `in` se valida y un error tipográfico
  en una comparación se detecta.

## Errores frecuentes

| Síntoma | Causa y solución |
|---|---|
| Un hueco no corta nada | Está fuera del `group` de la pieza, o no la toca. Revisa con el render. |
| Un hueco corta piezas que no debía | Está en el nivel superior: mételo en el `group` correcto. |
| La pieza aparece desplazada tras rotar | Se movió antes de rotar. Usa `.rotate(...).move(...)`. |
| `bbox min` con z < 0 | Algo queda bajo la cama. Ajusta posiciones o añade `.drop()` al grupo final. |
| `watertight: no` | Contactos solo por arista/punto o coincidencias casi exactas. Añade solapes de 0.5 mm. |
| Formas facetadas | Sube `$sides` (o `sides=` en la primitiva). |
| `unknown shape`, `has no parameter` | El mensaje sugiere el nombre correcto; consulta `tcad shapes`. |
