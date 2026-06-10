package graph

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// goASTProvider parses Go with the standard library — precise symbols and
// syntactically exact call edges, no CGo. This is the Go "precise" path: it
// finds more than the tree-sitter Go grammar (var/const, no error trees) and is
// the reference backend for the design's go/ast special case.
type goASTProvider struct{}

func (goASTProvider) Name() string { return "go/ast" }

func (goASTProvider) Tier(lang string) Tier {
	if lang == "go" {
		return TierPrecise
	}
	return TierUnsupported
}

// Extract parses each Go file in-process (go/ast has no batch advantage).
func (goASTProvider) Extract(_ context.Context, _, _ string, files []SourceFile) (map[string]FileResult, error) {
	out := make(map[string]FileResult, len(files))
	for _, f := range files {
		out[f.Rel] = parseGo(f.Rel, f.Data)
	}
	return out, nil
}

// parseGo extracts symbols and call edges from one Go file. A syntax error in
// one file must not abort the build: it returns an empty result plus a diagnostic.
func parseGo(path string, src []byte) FileResult {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return FileResult{Diagnostics: []string{err.Error()}}
	}
	line := func(p token.Pos) int { return fset.Position(p).Line }
	var res FileResult

	addCalls := func(enclosing string, body ast.Node) {
		if body == nil {
			return
		}
		ast.Inspect(body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var callee string
			switch fn := ce.Fun.(type) {
			case *ast.Ident:
				callee = fn.Name
			case *ast.SelectorExpr:
				callee = fn.Sel.Name
			}
			if callee != "" {
				res.Edges = append(res.Edges, Edge{
					SrcName: enclosing, DstName: callee, Kind: EdgeCall, Line: line(ce.Pos()),
				})
			}
			return true
		})
	}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			kind := KindFunc
			if d.Recv != nil && len(d.Recv.List) > 0 {
				kind = KindMethod
			}
			res.Symbols = append(res.Symbols, Symbol{
				Name: d.Name.Name, Kind: kind, Signature: goFuncSig(d),
				StartLine: line(d.Pos()), EndLine: line(d.End()),
			})
			addCalls(d.Name.Name, d.Body)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					kind := KindType
					switch sp.Type.(type) {
					case *ast.StructType:
						kind = KindStruct
					case *ast.InterfaceType:
						kind = KindInterface
					}
					res.Symbols = append(res.Symbols, Symbol{
						Name: sp.Name.Name, Kind: kind, Signature: string(kind) + " " + sp.Name.Name,
						StartLine: line(sp.Pos()), EndLine: line(sp.End()),
					})
				case *ast.ValueSpec:
					kind := KindVar
					if d.Tok == token.CONST {
						kind = KindConst
					}
					for _, nm := range sp.Names {
						if nm.Name == "_" {
							continue
						}
						res.Symbols = append(res.Symbols, Symbol{
							Name: nm.Name, Kind: kind, Signature: string(kind) + " " + nm.Name,
							StartLine: line(nm.Pos()), EndLine: line(nm.End()),
						})
					}
				}
			}
		}
	}
	return res
}

// goFuncSig renders a one-line signature skeleton (no body, arity-only params)
// for the repo map.
func goFuncSig(d *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		b.WriteString("(r) ")
	}
	b.WriteString(d.Name.Name)
	b.WriteString("(")
	if d.Type.Params != nil {
		n := 0
		for _, p := range d.Type.Params.List {
			c := len(p.Names)
			if c == 0 {
				c = 1
			}
			n += c
		}
		b.WriteString(strings.TrimSuffix(strings.Repeat("_, ", n), ", "))
	}
	b.WriteString(")")
	return b.String()
}
