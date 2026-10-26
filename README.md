# TCAD Studio — modelado 3D por sólidos y huecos, al estilo Tinkercad

TCAD es un lenguaje (DSL) para describir modelos 3D combinando las primitivas
de Tinkercad —cajas, cilindros, esferas, conos, toroides, texto...— como
**sólidos** o **huecos**. `tcad` es la herramienta en Go que lee ficheros
`.tcad` y genera **STL** listos para imprimir y **PNG** de previsualización.

```tcad
param ancho = 60
param fondo = 40
param alto  = 25
param pared = 2

group {
  box(ancho, fondo, alto, radius=3)
  hole box(ancho - 2*pared, fondo - 2*pared, alto, radius=1).move(z=pared)
  hole text("TCAD", size=8, height=1).rotate(x=90).move(y=-fondo/2 + 0.6, z=alto/2)
}
```

```bash
tcad build caja.tcad -o caja.stl
tcad render caja.tcad -o caja.png
tcad build caja.tcad -D ancho=80 -D alto=30   # cambiar parámetros sin editar
```

## Instalación

Requiere Go 1.26 o superior.

```bash
make                # compila bin/tcad
make install        # lo instala en $GOPATH/bin
make test           # tests (make race: con el detector de carreras)
make check          # formato + vet + tests + compilar los ejemplos (para CI)
make examples       # STL de examples/ en out/  (make renders: los PNG)
make watch MODEL=diseños/pieza.tcad   # visor en vivo en el navegador
make watch MODEL="-R diseños"         # ...con todos los diseños, para elegir en la página
make studio         # compila la app de escritorio bin/tcad-studio (TCAD Studio)
make run-studio FOLDER=diseños        # ...y la abre en esa carpeta
make help           # todos los objetivos
```

Sin `make`: `go build -o bin/tcad ./cmd/tcad`. `tcad` es Go puro (sin cgo).
TCAD Studio (`cmd/tcad-studio`) usa [Wails 3](https://v3.wails.io), que necesita
cgo y, en Linux, GTK3 y WebKitGTK 4.1 (`libgtk-3-dev libwebkit2gtk-4.1-dev`):
`go build -tags gtk3 -o bin/tcad-studio ./cmd/tcad-studio`. Para usar el GTK4 /
WebKitGTK 6.0 por defecto de Wails, `make studio GO_TAGS=`.

## Comandos

| Comando | Descripción |
|---|---|
| `tcad build modelo.tcad [-o x.stl] [-ascii]` | Genera el STL (binario por defecto). |
| `tcad render modelo.tcad [-o x.png] [-view all] [-size 500]` | Previsualización: hoja 2x2 (iso, frontal, derecha, planta) o una vista (`iso`, `iso-back`, `iso-below`, `front`, `back`, `left`, `right`, `top`, `bottom`). |
| `tcad info modelo.tcad` | Dimensiones, caja envolvente, volumen, nº de triángulos, si la malla es estanca y valores de los `param`. |
| `tcad check modelo.tcad` | Solo valida el fichero. |
| `tcad watch ruta... [-R] [-addr localhost:8080] [-o x.stl] [-no-stl] [-open]` | Visor 3D en el navegador que se recompila y recarga solo al guardar. Acepta varios ficheros y directorios. Ver abajo. |
| `tcad studio [carpeta] [-addr 127.0.0.1:0] [-open]` | TCAD Studio en el navegador (vista previa + agente). Ver abajo. |
| `tcad import pieza.stl [-o x.tcad] [-mode shapes\|embed\|ref] [-center]` | Convierte un STL (binario o ASCII) en un modelo TCAD. Ver abajo. |
| `tcad shapes` | Lista de primitivas con sus parámetros y valores por defecto. |

Todos aceptan `-D nombre=valor` (repetible) para sobrescribir `param`, y `-`
como fichero para leer de la entrada estándar.

## Visor en vivo: `tcad watch`

```bash
tcad watch diseños/soporte.tcad -open
# watching diseños/soporte.tcad, viewer at http://127.0.0.1:8080
```

Abre una página con un visor 3D (three.js) del modelo. Cada vez que guardas el
`.tcad` (o un STL que use con `stl("...")`), `tcad` lo recompila, reescribe el
STL (`diseños/soporte.stl`, o el que indiques con `-o`) y el navegador recarga
la pieza al instante sin perder el punto de vista. Si el fichero tiene un error,
la página lo muestra con su línea y columna y sigue enseñando la última versión
válida.

- Ratón: girar (botón izquierdo), desplazar (derecho), zoom (rueda).
- Teclas: `F` encajar la vista, `E` aristas, `W` malla de triángulos, `G` cama.
- Muestra las medidas, el volumen, los triángulos y si la malla es estanca, y
  respeta los `.color()` del modelo.
- "Descargar STL" baja el STL estándar.
- **Panel de parámetros**: cada `param` del modelo aparece con un control
  (deslizador, campo numérico, casilla, texto o desplegable). Al cambiarlo, el
  visor envía el valor a `tcad watch`, que recompila al momento con ese valor y
  reescribe el STL, sin tocar el `.tcad`. Los parámetros cambiados se marcan
  con ●, se pueden restablecer uno a uno o todos, y "Copiar -D" copia los
  valores como opciones para `tcad build`.
- El comentario de un `param` define su control y su descripción, como en el
  Customizer de OpenSCAD:

  ```tcad
  param alto   = 30      // [10:100] Altura de la caja (mm)
  param pared  = 2       // [1.2:0.2:4] Grosor de pared     ([mín:paso:máx])
  ```
- Los parámetros con un conjunto cerrado de valores se declaran en el propio
  lenguaje con `in`, y salen como desplegable:

  ```tcad
  param modo = "montado" in ["montado", "impresion"]   // Vista o piezas sueltas
  ```

  A diferencia de los rangos, esto se comprueba: un `-D modo=impresión` o una
  comparación `modo == "impresión"` dan error en vez de caer en otra rama.
- three.js va incluido en el binario: funciona sin conexión.
- **Varios modelos**: se le pueden pasar varios ficheros y directorios, y con
  `-R` también los subdirectorios. La página muestra entonces un desplegable
  (agrupado por directorio) para elegir el modelo; `N` / `Mayús+N` pasan al
  siguiente o al anterior. Solo se compila el modelo elegido, cada uno guarda
  sus propios cambios de parámetros mientras saltas entre ellos, y los
  `.tcad` nuevos que aparezcan en los directorios se añaden a la lista solos.
  Cada STL se escribe junto a su modelo (`-o` solo vale con un único modelo).

  ```bash
  tcad watch -R diseños -open                       # todos los diseños
  tcad watch diseños/piernas_lego.tcad examples     # un fichero + un directorio
  ```
- `-no-stl` solo sirve el visor; `-addr 0.0.0.0:8080` lo abre a la red local.
- Los `-D` iniciales de `tcad watch` se cargan como valores del panel.

La página habla con `tcad watch` mediante una API JSON que también se puede
usar desde scripts:

| Petición | Efecto |
|---|---|
| `GET /api/state` | Estado: modelo mostrado (`model`, `models`), última compilación, medidas, `params` (tipo, valor, valor por defecto, anotaciones) y `overrides`. |
| `GET /api/model`, `PUT /api/model` con `{"model": "examples/maceta.tcad"}` | Modelo mostrado y lista de modelos; elegir otro (400 si no está en la lista). |
| `PUT /api/params` con `{"alto": "45", "fuente": "\"mono\"", "pared": null}` | Cambia valores (expresiones TCAD, como en `-D`; `null` quita el cambio). Valida nombre y tipo (400 si no encajan) y recompila. |
| `DELETE /api/params` | Vuelve a los valores del fichero. |
| `GET /events` | Server-Sent Events con el estado tras cada compilación. |
| `GET /model.stl`, `GET /download.stl` | El modelo con colores (para el visor) y el STL estándar. |

## TCAD Studio: diseñar con un agente

TCAD Studio es una aplicación de escritorio (Wails 3) para diseñar piezas
hablando con un agente, inspirada en Kino Studio:

- **Izquierda, vista previa**: el visor de `tcad watch` con todos los modelos de
  la carpeta abierta y sus subcarpetas (desplegable, panel de parámetros,
  descarga del STL). **Salta solo al fichero que se crea o se guarda**, así
  que ves cada cambio del agente en cuanto lo hace.
- **Derecha, agente**: un terminal con Claude Code, Codex, Gemini CLI,
  Antigravity o una shell, ejecutándose en la carpeta. La barra entre los dos
  paneles se arrastra para repartir el espacio.

```bash
make run-studio                 # pantalla de inicio: abrir carpeta / recientes
bin/tcad-studio diseños         # directamente en una carpeta
tcad studio diseños -open       # la misma aplicación en el navegador, sin Wails
```

Una carpeta es un proyecto: ahí creas (o pides al agente que cree) los
ficheros y subcarpetas de cada pieza o conjunto. Al abrirla, TCAD Studio
escribe en ella lo que necesita el agente:

| Fichero | Qué es |
|---|---|
| `.claude/skills/tcad/` | El skill de TCAD (lenguaje, referencia y recetas), actualizado en cada apertura. |
| `AGENTS.md` | Instrucciones para el agente: cómo funciona la vista previa, cómo comprobar su trabajo, reglas. Solo se gestiona un bloque marcado; lo que escribas fuera se conserva. |
| `CLAUDE.md`, `GEMINI.md` | `@AGENTS.md`, solo si no existen. |
| `.tcad/preview.json` | Lo que el usuario está viendo: modelo, errores, medidas, `params` y valores cambiados en el panel. Así el agente sabe qué es "esta pieza". Lleva su propio `.gitignore`. |

El terminal del agente tiene `tcad` en el `PATH` (el propio `tcad-studio`
hace de `tcad`: `tcad-studio build x.tcad` funciona igual), así que puede
validar y renderizar sus piezas sin instalar nada más.

Cómo está hecho: `internal/studio` es un servidor local (127.0.0.1, puerto
libre) que sirve la página, monta el visor de `internal/watch` en `/preview/`
para la carpeta abierta y lleva los terminales del agente (`internal/terminal`,
PTYs). La ventana de Wails solo muestra ese servidor y aporta el diálogo
nativo para elegir carpeta; `tcad studio` sirve lo mismo para un navegador,
con un explorador de carpetas propio. Cada sesión tiene un token aleatorio:
la URL inicial lo lleva, la página lo guarda en una cookie `SameSite=Strict`
y el servidor rechaza peticiones sin él, de otros orígenes o con otro `Host`.

## Importar STL

```bash
tcad import pieza.stl                 # genera pieza.tcad
```

Hay tres modos de conversión:

- **`shapes`** (por defecto) reconstruye cada cuerpo con primitivas siempre que
  puede. Separa la malla en cuerpos conexos y analiza cada uno:
  - las esferas teseladas pasan a `sphere`;
  - las piezas que son extrusiones apiladas a lo largo de cualquier eje
    (placas, engranajes, cajas de esquinas vivas, perfiles...) se rehacen capa
    a capa: cada contorno se convierte en `box`, `cylinder`, `polygon` o
    `extrude`, y sus agujeros pasantes en `hole`, recuperando también la
    rotación 3D;
  - las cavidades internas pasan a `hole`;
  - lo que no se puede describir así (superficies curvas libres, redondeos,
    toroides...) se conserva exacto como `polyhedron`.
- **`embed`** copia la malla exacta en un `polyhedron` dentro del `.tcad`.
- **`ref`** escribe solo `stl(file="pieza.stl", center=false)`, una referencia
  al fichero original, como el "Importar" de Tinkercad.

`-center` lleva la pieza al plano de trabajo (centrada en XY, z = 0). Tras
convertir, `tcad import` evalúa el modelo generado y compara su volumen con el
del STL original.

Ejemplo: el STL del engranaje de `examples/` (18 790 triángulos, 635 KB en modo
`embed`) se convierte en 13 líneas legibles:

```tcad
group {
  extrude(points=[...contorno de los dientes...], height=6)
  hole cylinder(radius=4, height=8, sides=64).move(x=11, z=-1)
  ...
  hole polygon(sides=6, radius=4, height=8).move(z=-1)
}
group {
  cylinder(radius=7, height=1, sides=64)
  hole cylinder(radius=5, height=3, sides=64).move(z=-1)
}.move(z=6)
```

Los STL también se pueden usar directamente en un modelo, como sólido o como
hueco: `hole stl("tornillo.stl").move(z=5)`.

## El lenguaje en un minuto

- **Primitivas** (con los parámetros de Tinkercad): `box`, `cylinder`, `cone`,
  `sphere`, `half_sphere`, `torus`, `tube`, `paraboloid`, `wedge`, `roof`,
  `round_roof`, `pyramid`, `polygon`, `star`, `heart`, `icosahedron`, `text`,
  y además `extrude` (polígono 2D propio), `revolve` (perfil de revolución),
  `stl` (importar un fichero) y `polyhedron` (malla arbitraria).
- Cada primitiva nace **centrada en X/Y y apoyada en z = 0**, como al soltarla en
  el plano de trabajo de Tinkercad. Unidades en mm.
- `hole` convierte una forma en hueco. `group { ... }` une los sólidos y les resta
  los huecos (sin importar el orden); `intersect { ... }` deja solo lo común.
- Modificadores encadenables: `.move() .rotate() .scale() .mirror() .drop() .center() .color()`.
- `param`, `let`, `for x in range(n)`, `if/else`, `module` (componentes con
  parámetros), `func` (funciones de una expresión), listas, operadores, funciones
  matemáticas en grados y `$sides`/`$steps` para la resolución de las curvas.

La especificación completa está en
[`.claude/skills/tcad/reference.md`](.claude/skills/tcad/reference.md) y hay
recetas (tornillería, tapas, ranuras, matrices, texto...) en
[`.claude/skills/tcad/patterns.md`](.claude/skills/tcad/patterns.md).
En [`examples/`](examples) hay modelos completos: catálogo de primitivas, caja
con tapa, llavero, engranaje, maceta con plato y soporte para móvil.

## Skill para Claude

`.claude/skills/tcad/` contiene un skill que enseña a Claude el lenguaje y un
flujo de trabajo de diseño: escribir el modelo, validarlo con `tcad info`,
renderizarlo y **mirar la imagen** para corregir errores, y generar el STL. Claude
Code lo carga automáticamente al trabajar en este repositorio. Para usarlo en
cualquier proyecto, copia la carpeta a `~/.claude/skills/tcad` y asegúrate de
que `tcad` esté en el `PATH` (`go install ./cmd/tcad`).

## Cómo funciona

```
fichero .tcad ──► lexer/parser ──► AST ──► intérprete ──► mallas por primitiva
                  (internal/dsl)            │
                                            ▼
                         CSG por árboles BSP (internal/csg)
                         unión / diferencia / intersección
                                            │
                                            ▼
                soldado de vértices + reparación de T-junctions (internal/stl)
                                            │
                         ┌──────────────────┴──────────────────┐
                         ▼                                     ▼
                   STL binario/ASCII                PNG (rasterizador software,
                                                     internal/render)
```

- `internal/geom`: vectores, transformaciones afines y mallas de triángulos.
- `internal/shapes`: generadores de cada primitiva (revoluciones, extrusiones,
  caja redondeada, texto a partir de las fuentes Go mediante `x/image/font/sfnt`).
- `internal/csg`: booleanas con BSP (basado en csg.js) recortando solo los
  polígonos cercanos al otro operando, lo que reduce mucho la fragmentación.
- `internal/importer`: conversión de mallas a TCAD (detección de cuerpos,
  esferas y extrusiones apiladas por cortes a media altura de cada capa).
- `internal/stl`: lectura/escritura STL y post-proceso para que la malla sea estanca
  (`tcad info` lo verifica contando aristas no compartidas por dos triángulos).
- `internal/render`: z-buffer con supermuestreo, sombreado y detección de aristas.
- `internal/model`: carga de un `.tcad` (parseo, evaluación y malla lista para
  escribir); la comparten la línea de comandos y el estudio.
- `internal/watch`: visor en vivo. Descubre los modelos de las rutas indicadas,
  vigila los ficheros por sondeo, recompila el elegido, avisa a la página por
  Server-Sent Events y le sirve un STL con colores por faceta. Es un
  `http.Handler` reutilizable (`tcad watch` lo sirve solo; el estudio lo monta
  bajo `/preview/`). Incluye three.js r160 (licencia MIT, en
  `internal/watch/assets`).
- `internal/cli`: los comandos de `tcad` (`cmd/tcad` y `cmd/tcad-studio` los ejecutan).
- `internal/studio`: el backend de TCAD Studio (carpetas, sesión, terminales del
  agente) y su página. Incluye xterm.js 6 (licencia MIT, en `internal/studio/assets`).
- `internal/terminal`, `internal/agentctx`: PTYs de los agentes y los ficheros
  de contexto que se escriben en cada carpeta.

## Limitaciones

- No hay `hull` ni `minkowski`; las formas redondeadas se consiguen con los
  parámetros `radius`/`bevel` o combinando primitivas (ver `patterns.md`).
- El CSG trabaja en coma flotante: en casos muy degenerados (muchas piezas que se
  cruzan exactamente en el mismo eje) pueden quedar unas pocas aristas abiertas
  de tamaño microscópico; `tcad` lo avisa y los laminadores lo reparan solos.
- Mallas muy densas (esferas con `steps` altos, `$sides` > 128) hacen el CSG lento.
- `tcad import` no descompone formas con redondeos o superficies libres en
  operaciones CSG: esos cuerpos quedan como `polyhedron` exacto (editable con
  huecos y sólidos añadidos, pero no paramétrico).
- Las fuentes disponibles para `text` son las Go fonts (sans y mono, con
  variantes negrita y cursiva).
