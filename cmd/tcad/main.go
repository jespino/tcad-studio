// Command tcad compiles TCAD model files (a CSG language of solids and holes) into
// STL meshes and PNG previews. See internal/cli.
package main

import (
	"os"

	"github.com/jespino/tcad-studio/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:])) }
