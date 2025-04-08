package parser

import (
	"log"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/tspath"
)

func getPragmaArgument(pragma *ast.Pragma, name string) string {
	if pragma != nil {
		if arg, ok := pragma.Args[name]; ok {
			return arg.Value
		}
	}
	return ""
}

func GetPragmaFromSourceFile(file *ast.SourceFile, name string) *ast.Pragma {
	if file != nil {
		for i := range file.Pragmas {
			if file.Pragmas[i].Name == name {
				return &file.Pragmas[i]
			}
		}
	}
	return nil
}

func GetJSXImplicitImportBase(compilerOptions *core.CompilerOptions, file *ast.SourceFile) string {
	jsxImportSourcePragma := GetPragmaFromSourceFile(file, "jsximportsource")
	jsxRuntimePragma := GetPragmaFromSourceFile(file, "jsxruntime")
	if getPragmaArgument(jsxRuntimePragma, "factory") == "classic" {
		return ""
	}
	if compilerOptions.Jsx == core.JsxEmitReactJSX ||
		compilerOptions.Jsx == core.JsxEmitReactJSXDev ||
		compilerOptions.JsxImportSource != "" ||
		jsxImportSourcePragma != nil ||
		getPragmaArgument(jsxRuntimePragma, "factory") == "automatic" {
		result := getPragmaArgument(jsxImportSourcePragma, "factory")
		if result == "" {
			result = compilerOptions.JsxImportSource
		}
		if result == "" {
			result = "react"
		}
		return result
	}
	return ""
}

func GetJSXRuntimeImport(base string, options *core.CompilerOptions) string {
	if base == "" {
		return base
	}
	return base + "/" + core.IfElse(options.Jsx == core.JsxEmitReactJSXDev, "jsx-dev-runtime", "jsx-runtime")
}

func (p *Parser) createSyntheticImport(text string, file *ast.SourceFile) *ast.Node {
	externalHelpersModuleReference := p.factory.NewStringLiteral(text)
	importDecl := p.factory.NewImportDeclaration(nil /*modifiers*/, nil /*importClause*/, externalHelpersModuleReference, nil)
	// !!!
	// addInternalEmitFlags(importDecl, InternalEmitFlags.NeverApplyImportHelper);
	externalHelpersModuleReference.Parent = importDecl
	importDecl.Parent = file.AsNode()
	// explicitly unset the synthesized flag on these declarations so the checker API will answer questions about them
	// (which is required to build the dependency graph for incremental emit)
	externalHelpersModuleReference.Flags &= ^ast.NodeFlagsSynthesized
	importDecl.Flags &= ^ast.NodeFlagsSynthesized
	return externalHelpersModuleReference
}

func (p *Parser) collectExternalModuleReferences(file *ast.SourceFile, options *core.CompilerOptions) {
	isJavaScriptFile := ast.IsInJSFile(file.AsNode())
	isExternalModuleFile := ast.IsExternalModule(file)

	// If we are importing helpers, we need to add a synthetic reference to resolve the
	// helpers library. (A JavaScript file without `externalModuleIndicator` set might be
	// a CommonJS module; `commonJSModuleIndicator` doesn't get set until the binder has
	// run. We synthesize a helpers import for it just in case; it will never be used if
	// the binder doesn't find and set a `commonJSModuleIndicator`.)
	if options != nil && (isJavaScriptFile || (!file.IsDeclarationFile && (options.GetIsolatedModules() || isExternalModuleFile))) {
		if options.ImportHelpers.IsTrue() {
			// synthesize 'import "tslib"' declaration
			file.Imports = append(file.Imports, p.createSyntheticImport("tslib", file))
		}
		jsxImport := checker.GetJSXRuntimeImport(checker.GetJSXImplicitImportBase(options, file), options)
		if jsxImport != "" {
			// synthesize `import "base/jsx-runtime"` declaration
			file.Imports = append(file.Imports, p.createSyntheticImport(jsxImport, file))
		}
	}
	for _, node := range file.Statements.Nodes {
		collectModuleReferences(file, node, false /*inAmbientModule*/)
	}

	if file.Flags&ast.NodeFlagsPossiblyContainsDynamicImport != 0 || ast.IsInJSFile(file.AsNode()) {
		ast.ForEachDynamicImportOrRequireCall(file /*includeTypeSpaceImports*/, true /*requireStringLiteralLikeArgument*/, true, func(node *ast.Node, moduleSpecifier *ast.Expression) bool {
			ast.SetParentInChildren(node) // we need parent data on imports before the program is fully bound, so we ensure it's set here
			file.Imports = append(file.Imports, moduleSpecifier)
			return false
		})
	}
}

func collectModuleReferences(file *ast.SourceFile, node *ast.Statement, inAmbientModule bool) {
	if ast.IsAnyImportOrReExport(node) {
		moduleNameExpr := ast.GetExternalModuleName(node)
		// TypeScript 1.0 spec (April 2014): 12.1.6
		// An ExternalImportDeclaration in an AmbientExternalModuleDeclaration may reference other external modules
		// only through top - level external module names. Relative external module names are not permitted.
		if moduleNameExpr != nil && ast.IsStringLiteral(moduleNameExpr) {
			moduleName := moduleNameExpr.AsStringLiteral().Text
			if moduleName != "" && (!inAmbientModule || !tspath.IsExternalModuleNameRelative(moduleName)) {
				ast.SetParentInChildren(node) // we need parent data on imports before the program is fully bound, so we ensure it's set here
				file.Imports = append(file.Imports, moduleNameExpr)
				// !!! removed `&& p.currentNodeModulesDepth == 0`
				if file.UsesUriStyleNodeCoreModules != core.TSTrue && !file.IsDeclarationFile {
					if strings.HasPrefix(moduleName, "node:") && !exclusivelyPrefixedNodeCoreModules[moduleName] {
						// Presence of `node:` prefix takes precedence over unprefixed node core modules
						file.UsesUriStyleNodeCoreModules = core.TSTrue
					} else if file.UsesUriStyleNodeCoreModules == core.TSUnknown && unprefixedNodeCoreModules[moduleName] {
						// Avoid `unprefixedNodeCoreModules.has` for every import
						file.UsesUriStyleNodeCoreModules = core.TSFalse
					}
				}
			}
		}
		return
	}
	if ast.IsModuleDeclaration(node) && ast.IsAmbientModule(node) && (inAmbientModule || ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient) || file.IsDeclarationFile) {
		ast.SetParentInChildren(node)
		nameText := node.AsModuleDeclaration().Name().Text()
		// Ambient module declarations can be interpreted as augmentations for some existing external modules.
		// This will happen in two cases:
		// - if current file is external module then module augmentation is a ambient module declaration defined in the top level scope
		// - if current file is not external module then module augmentation is an ambient module declaration with non-relative module name
		//   immediately nested in top level ambient module declaration .
		if ast.IsExternalModule(file) || (inAmbientModule && !tspath.IsExternalModuleNameRelative(nameText)) {
			file.ModuleAugmentations = append(file.ModuleAugmentations, node.AsModuleDeclaration().Name())
		} else if !inAmbientModule {
			if file.IsDeclarationFile {
				// for global .d.ts files record name of ambient module
				file.AmbientModuleNames = append(file.AmbientModuleNames, nameText)
			}
			// An AmbientExternalModuleDeclaration declares an external module.
			// This type of declaration is permitted only in the global module.
			// The StringLiteral must specify a top - level external module name.
			// Relative external module names are not permitted
			// NOTE: body of ambient module is always a module block, if it exists
			if node.AsModuleDeclaration().Body != nil {
				for _, statement := range node.AsModuleDeclaration().Body.AsModuleBlock().Statements.Nodes {
					collectModuleReferences(file, statement, true /*inAmbientModule*/)
				}
			}
		}
	}
}

var unprefixedNodeCoreModules = map[string]bool{
	"assert":              true,
	"assert/strict":       true,
	"async_hooks":         true,
	"buffer":              true,
	"child_process":       true,
	"cluster":             true,
	"console":             true,
	"constants":           true,
	"crypto":              true,
	"dgram":               true,
	"diagnostics_channel": true,
	"dns":                 true,
	"dns/promises":        true,
	"domain":              true,
	"events":              true,
	"fs":                  true,
	"fs/promises":         true,
	"http":                true,
	"http2":               true,
	"https":               true,
	"inspector":           true,
	"inspector/promises":  true,
	"module":              true,
	"net":                 true,
	"os":                  true,
	"path":                true,
	"path/posix":          true,
	"path/win32":          true,
	"perf_hooks":          true,
	"process":             true,
	"punycode":            true,
	"querystring":         true,
	"readline":            true,
	"readline/promises":   true,
	"repl":                true,
	"stream":              true,
	"stream/consumers":    true,
	"stream/promises":     true,
	"stream/web":          true,
	"string_decoder":      true,
	"sys":                 true,
	"test/mock_loader":    true,
	"timers":              true,
	"timers/promises":     true,
	"tls":                 true,
	"trace_events":        true,
	"tty":                 true,
	"url":                 true,
	"util":                true,
	"util/types":          true,
	"v8":                  true,
	"vm":                  true,
	"wasi":                true,
	"worker_threads":      true,
	"zlib":                true,
}

var exclusivelyPrefixedNodeCoreModules = map[string]bool{
	"node:sea":            true,
	"node:sqlite":         true,
	"node:test":           true,
	"node:test/reporters": true,
}
