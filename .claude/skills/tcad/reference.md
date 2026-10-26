# Referencia del lenguaje TCAD

## Sistema de coordenadas

- Unidades: milímetros. Ángulos: grados.
- X = largo (izquierda → derecha), Y = fondo (el **frente** del modelo es -Y,
  la vista `front` mira desde -Y), Z = alto. El plano de trabajo / cama es z = 0.
- Cada primitiva se crea **centrada en X e Y y con su punto más bajo en z = 0**.
  Ejemplo: `box(20, 10, 5)` ocupa x∈[-10,10], y∈[-5,5], z∈[0,5].
  `extrude` y `revolve` son la excepción: respetan las coordenadas dadas.

## Estructura de un fichero

Un fichero es una lista de sentencias. El fichero completo se comporta como un
`group`: el STL final es la unión de los sólidos del nivel superior menos los
huecos del nivel superior. Los `;` al final de línea son opcionales.

Comentarios: `// línea`, `# línea`, `/* bloque */`.

### Sentencias

| Sentencia | Descripción |
|---|---|
| `param nombre = expr` | Parámetro configurable (`tcad ... -D nombre=valor` o desde el panel de `tcad watch`). Solo en el nivel superior. El valor nuevo debe ser del mismo tipo. Admite una anotación en el comentario de la misma línea (ver abajo). |
| `param nombre = expr in [v1, v2, ...]` | Parámetro con un **conjunto cerrado de valores**. El valor por defecto, los `-D` y los cambios del panel deben ser uno de ellos (si no, error con la lista). Comparar el parámetro con un literal que no está en la lista (`modo == "impresión"`) es un error. El panel lo muestra como desplegable. |
| `let nombre = expr` | Variable. Visible en su bloque y en los bloques anidados. Redefinirla en un bloque interior la oculta solo dentro de ese bloque. |
| `func nombre(a, b = 1) = expr` | Función pura de una expresión. Solo nivel superior; se puede usar antes de definirla; admite recursión. |
| `module nombre(a, b = 1) { ... }` | Componente reutilizable. Se invoca como una forma: `nombre(3, b=2)`. Su cuerpo es un grupo. Solo nivel superior. |
| `[solid\|hole] forma .mod(...) .mod(...)` | Coloca una forma. `solid` es el valor por defecto. |
| `for x in lista { ... }` | Repite el cuerpo; las formas se añaden al grupo que contiene el bucle. |
| `if cond { ... } else if cond { ... } else { ... }` | Condicional; igual que `for`, no crea grupo. |
| `echo(expr, nombre = expr, ...)` | Imprime valores por stderr para depurar. |

Dentro de un `module` solo se ven sus parámetros, sus `let` y las variables
globales (los `let`/`param` del nivel superior definidos **antes** de la
llamada). No ve las variables locales de quien lo llama: pásalas como argumentos.

### Valores permitidos y anotaciones de `param`

Cuando un parámetro solo puede tomar unos valores concretos (un modo, una
fuente, un tamaño de tornillo), decláralos con `in`. Es parte del lenguaje y se
comprueba:

```tcad
param modo   = "montado" in ["montado", "impresion"]   // Vista o piezas sueltas
param metrica = 3 in [2, 2.5, 3, 4, 5]                 // Tornillo M
param fuente = "sans" in ["sans", "mono"]
```

El comentario al final de la línea es una **anotación** para el panel de
`tcad watch` (como el Customizer de OpenSCAD): un rango opcional para el
deslizador y una descripción. Los rangos son solo una guía para la interfaz; no
limitan el valor al compilar.

```tcad
param ancho  = 60      // [20:200] Ancho exterior (mm)      → deslizador de 20 a 200
param pared  = 2       // [1.2:0.2:4] Grosor de pared       → [mín:paso:máx]
param tapa   = true    // Imprimir también la tapa          → casilla + descripción
```

Sin rango, los números se editan con un campo numérico; los `bool` con una
casilla; los parámetros con `in` con un desplegable.

### Formas

- `primitiva(args)` — ver tabla de primitivas.
- `nombre_modulo(args)` — instancia un módulo.
- `group { sentencias }` — resultado = ∪ sólidos − ∪ huecos (el orden no importa).
- `intersect { sentencias }` — resultado = ∩ sólidos − ∪ huecos.

Prefijo `hole`: la forma resultante es un hueco en el grupo que la contiene.
`hole group { ... }` convierte un grupo entero en hueco (útil para huecos
compuestos). Un grupo sin sólidos no produce nada.

### Argumentos

Posicionales en el orden de la tabla y/o con nombre: `box(20, 10, height=5)`.
Las primitivas con `length`, `width`, `height` aceptan también
`size=[l, w, h]`. Los alias (`r`, `h`, `l`, `w`...) equivalen al nombre largo.

### Modificadores

Se encadenan tras la forma, se aplican **en orden y alrededor del origen**.

| Modificador | Efecto |
|---|---|
| `.move(x, y, z)` / `.translate(...)` | Traslada. Admite nombres: `.move(z=5)`. |
| `.rotate(x, y, z)` | Gira en grados alrededor de los ejes del mundo: primero X, luego Y, luego Z. Para girar sobre el centro de la pieza, gírala antes de moverla. |
| `.scale(s)` / `.scale(x, y, z)` | Escala uniforme o por eje (desde el origen). |
| `.mirror("x")` | Refleja en el eje indicado (`"x"`, `"y"`, `"z"` o combinaciones `"xy"`). También `.mirror(x=true)`. |
| `.drop()` | Desplaza en Z para que el punto más bajo quede en z = 0. |
| `.center("xy")` | Centra la caja envolvente en los ejes indicados (por defecto `"xy"`; `"xyz"` centra también en Z). |
| `.color(c)` | Color de previsualización: nombre (`red orange yellow green blue purple pink white black gray brown cyan magenta teal navy lime`), `"#rrggbb"` o `r, g, b` (0–255). Las caras cortadas por un hueco toman el color del primer sólido del grupo. |

### Calidad de las curvas

`let $sides = N` fija el valor por defecto del parámetro `sides` (segmentos
alrededor de una circunferencia) y `let $steps = N` el de `steps` (segmentos de
perfiles curvos: esferas, redondeos...). Respetan el ámbito: un `let $sides`
dentro de un grupo o módulo solo afecta a ese bloque. Un argumento explícito
(`sides=8`) siempre gana.

## Expresiones

- Tipos: número (`3`, `2.5`, `1e-3`), cadena (`"texto"`, escapes `\n \t \" \\`),
  booleano (`true`, `false`), lista (`[1, 2, [3, 4]]`).
- Operadores, de menor a mayor precedencia:
  `?:` · `||` · `&&` · `== !=` · `< <= > >=` · `+ -` · `* / %` · unarios `- + !` · índice `x[i]`.
- `+` concatena cadenas (`"M" + 3` → `"M3"`) y listas (`[1] + [2]`).
- Índices negativos cuentan desde el final (`lista[-1]`).
- Verdad: `false`, `0`, `""` y `[]` son falsos.
- Constante: `PI`.

### Funciones incorporadas

| Función | Descripción |
|---|---|
| `sin cos tan` | Trigonometría en **grados**. |
| `asin acos atan atan2(y, x)` | Inversas, devuelven grados. |
| `sqrt abs floor ceil round sign exp ln pow(b, e)` | Matemáticas. |
| `min(a, b, ...)`, `max(...)` | También aceptan una lista: `max(lista)`. |
| `len(x)` | Longitud de lista o cadena. |
| `str(a, b, ...)` | Concatena valores como texto. |
| `range(n)`, `range(a, b)`, `range(a, b, paso)` | Lista `[a, a+paso, ...]` **sin incluir** `b`. `range(3)` → `[0, 1, 2]`. |

## Primitivas

Valores por defecto entre paréntesis. `sides`/`steps` marcados con * toman
`$sides`/`$steps` si están definidos.

| Primitiva | Parámetros (orden posicional) | Notas |
|---|---|---|
| `box` | `length`(20) `width`(20) `height`(20) `radius`(0) `steps`*(8) | `radius` redondea todas las aristas (como la Caja de Tinkercad); `steps` = segmentos del redondeo. |
| `cylinder` | `radius`(10) `height`(20) `sides`*(32) `bevel`(0) `segments`(1) | `sides=6` da un prisma hexagonal. `bevel` biselea arista superior e inferior; `segments=1` chaflán, >1 redondeo. |
| `cone` | `base_radius`(10) `top_radius`(0) `height`(20) `sides`*(32) | Con `top_radius > 0` es un cono truncado. Alias: `radius`/`r`/`r1`, `r2`. |
| `sphere` | `radius`(10) `steps`*(16) | Apoyada en el plano: centro en z = radius. |
| `half_sphere` | `radius`(10) `steps`*(16) | Cúpula; cara plana abajo. |
| `torus` | `radius`(10) `tube`(2) `sides`*(32) `steps`*(16) | `radius` = del centro al eje del tubo; tamaño total 2·(radius+tube). `tube < radius`. |
| `tube` | `radius`(10) `wall`(2) `height`(20) `sides`*(32) `bevel`(0) `segments`(1) | Cilindro hueco; `radius` exterior. Alias `thickness` = `wall`. |
| `paraboloid` | `radius`(10) `height`(20) `sides`*(32) `steps`*(16) | Base abajo, punta arriba. |
| `wedge` | `length`(20) `width`(20) `height`(20) | Cuña: perfil triangular en XZ extruido en Y. Cara vertical en -X, baja hacia +X. |
| `roof` | `length`(20) `width`(20) `height`(20) | Tejado: prisma triangular, la cumbrera va a lo largo de X. |
| `round_roof` | `length`(20) `width`(20) `height`(10) `sides`*(32) | Medio cilindro tumbado a lo largo de X. |
| `pyramid` | `length`(20) `width`(20) `height`(20) `sides`(4) | Con 4 lados base rectangular L×W; con otros valores, polígono regular. |
| `polygon` | `sides`(6) `radius`(10) `height`(20) | Prisma de polígono regular (radio a los vértices). |
| `star` | `points`(5) `radius`(10) `inner_radius`(5) `height`(5) | Estrella con una punta hacia +Y. |
| `heart` | `length`(20) `width`(20) `height`(5) | Corazón ajustado a L×W. |
| `icosahedron` | `radius`(10) | Radio a los vértices. |
| `text` | `text`("Text") `size`(10) `height`(3) `font`("sans") `spacing`(0) `align`("center") | `size` = altura de las mayúsculas en mm. Fuentes: `sans`, `sans-bold`, `sans-italic`, `sans-bold-italic`, `mono`, `mono-bold`. `\n` para varias líneas. El texto se lee desde arriba (+Z); centrado en XY. |
| `extrude` | `points`(obligatorio) `height`(10) | Polígono 2D `[[x, y], ...]` (simple, sin autointersecciones, cualquier sentido de giro) extruido de z=0 a `height`. **Conserva las coordenadas.** |
| `revolve` | `points`(obligatorio) `sides`*(32) | Perfil `[[r, z], ...]` con r ≥ 0 girado alrededor del eje Z. El perfil se cierra solo; los puntos con r = 0 quedan sobre el eje. **Conserva las coordenadas.** |
| `stl` | `file`(obligatorio) `center`(true) | Importa un STL (binario o ASCII). Ruta relativa al `.tcad`. Con `center=true` se centra en XY y se apoya en z = 0 como el Importar de Tinkercad; con `false` conserva las coordenadas. Se puede usar como sólido o `hole` y con modificadores. |
| `polyhedron` | `points`(obligatorio) `faces`(obligatorio) | Malla arbitraria: `points=[[x, y, z], ...]`, `faces=[[i, j, k], ...]` (índices desde 0, caras en sentido antihorario vistas desde fuera; se admiten polígonos). Debe ser cerrada. **Conserva las coordenadas.** Es lo que genera `tcad import` para cuerpos no reconocibles. |

## Semántica de la agrupación, con ejemplos

```tcad
// 1) Hueco dentro del grupo: solo afecta a ese grupo
group {
  box(30, 30, 10)
  hole cylinder(r=5, h=12).move(z=-1)
}
box(10, 10, 10).move(x=40)        // no se ve afectada por el hueco anterior

// 2) Hueco en el nivel superior: resta de todo el nivel superior
box(30, 30, 10)
box(10, 10, 10).move(x=20)
hole box(100, 4, 100).move(z=-1)  // corta ambas cajas

// 3) Hueco compuesto: se agrupa y se usa como un único hueco
group {
  box(40, 40, 10)
  hole group {
    cylinder(r=1.7, h=12)              // caña de un tornillo M3
    cylinder(r=3.2, h=4).move(z=8)     // alojamiento de la cabeza
  }.move(z=-1)                         // z de -1 a 11: atraviesa la caja
}

// 4) Los huecos de un módulo solo vacían el propio módulo
module arandela(d_ext, d_int, h) {
  cylinder(radius=d_ext/2, height=h)
  hole cylinder(radius=d_int/2, height=h + 2).move(z=-1)
}
arandela(20, 8, 2)
```

## Mensajes de error

Los errores indican `fichero:línea:columna` y, si hay un nombre mal escrito,
sugieren el correcto (`did you mean "cylinder"?`). Errores típicos:
`unknown shape`, `has no parameter`, `undefined variable`, `expects a number`,
`missing required parameter`, `the model is empty`.
