package exporter_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

var insertWithColumnsPattern = regexp.MustCompile(`(?is)^\s*insert\s+into\s+\S+\s*\(`)

func TestClickHouseInsertsSpecifyTargetColumns(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test source")
	}

	repoRoot := filepath.Dir(filepath.Dir(filename))
	for _, root := range []string{"exporter", "usage"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}

			fileset := token.NewFileSet()
			file, err := parser.ParseFile(fileset, path, nil, 0)
			if err != nil {
				return err
			}

			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}

				value, err := strconv.Unquote(literal.Value)
				if err != nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "insert into ") {
					return true
				}
				if !insertWithColumnsPattern.MatchString(value) {
					position := fileset.Position(literal.Pos())
					t.Errorf("%s:%d: ClickHouse INSERT must specify target columns: %q", position.Filename, position.Line, value)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", root, err)
		}
	}
}
