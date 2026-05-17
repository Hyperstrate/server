package application

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestStreamingWrappersUseContextAwareSends(t *testing.T) {
	files := []string{
		"service.go",
		filepath.Join("..", "module.go"),
		filepath.Join("..", "..", "ai", "application", "service.go"),
	}

	for _, path := range files {
		t.Run(path, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}

			var stack []ast.Node
			ast.Inspect(file, func(node ast.Node) bool {
				if node == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				if send, ok := node.(*ast.SendStmt); ok {
					if ident, ok := send.Chan.(*ast.Ident); ok && ident.Name == "out" {
						inSelect := false
						for _, parent := range stack {
							if _, ok := parent.(*ast.SelectStmt); ok {
								inSelect = true
								break
							}
						}
						if !inSelect {
							t.Fatalf("plain send to out at %s; wrap streaming sends in select with ctx.Done()", fset.Position(send.Pos()))
						}
					}
				}
				stack = append(stack, node)
				return true
			})
		})
	}
}
