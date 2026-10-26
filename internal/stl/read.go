package stl

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/jespino/tcad-studio/internal/geom"
)

// ReadFile loads a binary or ASCII STL file.
func ReadFile(path string) (*geom.Mesh, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// Parse decodes STL data. Binary files may also start with "solid", so the
// size is used to tell both formats apart.
func Parse(data []byte) (*geom.Mesh, error) {
	if len(data) >= 84 {
		n := binary.LittleEndian.Uint32(data[80:84])
		if uint64(len(data)) == 84+50*uint64(n) {
			return parseBinary(data, int(n))
		}
	}
	if bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("solid")) {
		return parseASCII(data)
	}
	if len(data) >= 84 {
		return nil, fmt.Errorf("invalid binary STL: size does not match the triangle count")
	}
	return nil, fmt.Errorf("not an STL file")
}

func parseBinary(data []byte, n int) (*geom.Mesh, error) {
	m := &geom.Mesh{Tris: make([]geom.Triangle, 0, n)}
	f := func(off int) float64 {
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(data[off:])))
	}
	for i := 0; i < n; i++ {
		base := 84 + 50*i + 12 // skip the stored normal, it is recomputed
		var t geom.Triangle
		for k := 0; k < 3; k++ {
			o := base + 12*k
			t.V[k] = geom.V(f(o), f(o+4), f(o+8))
		}
		m.Tris = append(m.Tris, t)
	}
	return m, nil
}

func parseASCII(data []byte) (*geom.Mesh, error) {
	m := &geom.Mesh{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	var verts []geom.Vec3
	line := 0
	for sc.Scan() {
		line++
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "vertex":
			if len(fields) != 4 {
				return nil, fmt.Errorf("line %d: malformed vertex", line)
			}
			var c [3]float64
			for i := range c {
				v, err := strconv.ParseFloat(fields[i+1], 64)
				if err != nil {
					return nil, fmt.Errorf("line %d: %v", line, err)
				}
				c[i] = v
			}
			verts = append(verts, geom.V(c[0], c[1], c[2]))
		case "endloop":
			if len(verts) < 3 {
				return nil, fmt.Errorf("line %d: facet with fewer than 3 vertices", line)
			}
			for i := 1; i+1 < len(verts); i++ {
				m.Tris = append(m.Tris, geom.Triangle{V: [3]geom.Vec3{verts[0], verts[i], verts[i+1]}})
			}
			verts = verts[:0]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(m.Tris) == 0 {
		return nil, fmt.Errorf("the STL file has no triangles")
	}
	return m, nil
}
