# Proyecto TCAD

Esta carpeta es un proyecto de **TCAD Studio**. Eres el agente que corre en el
panel de la derecha; a la izquierda el usuario ve una vista previa 3D en vivo
de los modelos `.tcad` de esta carpeta y sus subcarpetas.

## Cómo se trabaja

- Los modelos se escriben en el lenguaje TCAD. Antes de tu primer modelo lee el
  skill `.claude/skills/tcad/SKILL.md` y su referencia (`reference.md`); las
  recetas de `patterns.md` cuando haya encajes, tornillería, texto o
  repeticiones.
- Cada pieza es un fichero `.tcad`. Un proyecto de varias piezas va en su
  propia subcarpeta (por ejemplo `estacion/base.tcad`, `estacion/tapa.tcad`).
  Usa nombres de fichero en minúsculas, sin espacios.
- La vista previa recompila al guardar y **salta sola al fichero que acabas de
  crear o guardar**, así que el usuario ve cada cambio al momento. No ejecutes
  `tcad watch`: la vista ya está abierta. El usuario también puede elegir otro
  modelo en el desplegable de la vista previa.
- La herramienta `tcad` está en el PATH de esta terminal: `tcad check`,
  `tcad info`, `tcad render` (PNG para revisar tú el resultado con Read),
  `tcad build` (STL) y `tcad import` (STL → TCAD). Deja los PNG y STL de
  pruebas fuera de la carpeta del proyecto (por ejemplo en `/tmp`) salvo que
  el usuario los pida.

## Qué está viendo el usuario

Tras cada compilación, TCAD Studio escribe **`.tcad/preview.json`**:

```json
{
  "model": "estacion/tapa.tcad",
  "ok": false,
  "error": "/ruta/a/la/carpeta/estacion/tapa.tcad:12:3: unknown shape \"bx\" (did you mean \"box\"?)",
  "size": [80, 60, 4],
  "params": [{ "name": "ancho", "value": 90, "default": 80, "overridden": true }],
  "overrides": { "ancho": "90" }
}
```

- `model` es el modelo abierto en la vista previa (relativo a esta carpeta):
  cuando el usuario hable de "esta pieza" o "lo que estoy viendo", es ese.
- `ok`/`error` es el resultado de la última compilación de ese modelo.
- `overrides` son los valores que el usuario ha cambiado en el panel de
  parámetros sin tocar el fichero. Si te pide quedarse con ellos, escríbelos
  como valor del `param` en el `.tcad`.

## Reglas

1. Lee el fichero antes de editarlo; respeta los nombres, comentarios y estilo
   del usuario.
2. Tras cada cambio comprueba que compila (`tcad check`, o `.tcad/preview.json`
   si es el modelo que se está viendo) y revisa visualmente con `tcad render`
   cuando cambie la forma.
3. Expón como `param` (con su comentario `// [min:max] descripción`, o
   `in [...]` para valores cerrados) las medidas que el usuario querrá ajustar:
   aparecen como controles en la vista previa.
4. No borres ficheros del usuario salvo que te lo pida.
