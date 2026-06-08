package graph

// Registry maps a language label to the ParserProvider that handles it.
type Registry struct{ byLang map[string]ParserProvider }

func (r *Registry) register(p ParserProvider, langs ...string) {
	for _, l := range langs {
		r.byLang[l] = p
	}
}

// For returns the provider registered for a language label, if any.
func (r *Registry) For(lang string) (ParserProvider, bool) {
	p, ok := r.byLang[lang]
	return p, ok
}

// DefaultRegistry wires the providers available in this build:
//   - Go via go/ast (precise, in-process);
//   - every other supported language via ast-grep (real tree-sitter scanners),
//     when the ast-grep binary is present.
//
// If ast-grep is not installed, the non-Go languages are left unregistered and
// Build counts them as Unsupported (skipped) rather than mis-parsing them.
func DefaultRegistry() *Registry {
	r := &Registry{byLang: map[string]ParserProvider{}}
	r.register(goASTProvider{}, "go")
	if ag, ok := newASTGrep(); ok {
		for lang := range agByLang {
			r.register(ag, lang)
		}
	}
	return r
}

// ASTGrepAvailable reports whether orchard can use the ast-grep backend for
// non-Go languages. It follows the same resolution order as DefaultRegistry.
func ASTGrepAvailable() bool {
	_, ok := newASTGrep()
	return ok
}

// ASTGrepSupports reports whether ast-grep is orchard's parser backend for the
// given graph language label.
func ASTGrepSupports(lang string) bool {
	_, ok := agByLang[lang]
	return ok
}
