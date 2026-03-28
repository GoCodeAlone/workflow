package module

import (
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/expr-lang/expr"
)

// ExprEngine evaluates ${ ... } expressions against a PipelineContext.
// It provides a simpler, more natural syntax than Go templates:
//
//	${ body.name }                     – field access
//	${ upper(body.name) }              – function call
//	${ steps["parse"]["id"] }          – hyphenated step names
//	${ x == "active" && y > 5 }        – boolean / comparison
//	${ "Hello " + body.name }          – string concat
type ExprEngine struct{}

// NewExprEngine creates a new ExprEngine.
func NewExprEngine() *ExprEngine {
	return &ExprEngine{}
}

// exprEnv builds the evaluation environment from a PipelineContext,
// merging data fields and all registered functions.
func (e *ExprEngine) exprEnv(pc *PipelineContext) map[string]any {
	env := make(map[string]any)

	// Functions registered first; everything below overrides name conflicts.
	maps.Copy(env, templateFuncMap())

	// Context-aware config lookup.
	env["config"] = func(key string) string {
		if v, ok := GetConfigRegistry().Get(key); ok {
			return v
		}
		return ""
	}

	// Current merged state at top level (overrides same-named functions).
	maps.Copy(env, pc.Current)

	// Named namespaces always win — they override same-named data fields so that
	// steps["x"], trigger, body, meta are always namespace references.
	env["steps"] = pc.StepOutputs
	env["trigger"] = map[string]any(pc.TriggerData)
	env["body"] = map[string]any(pc.TriggerData) // alias for trigger
	env["meta"] = pc.Metadata
	env["current"] = pc.Current

	return env
}

// Evaluate evaluates a single expr expression string (without ${ } delimiters)
// against the provided PipelineContext and returns the result as a string.
// Nil results become empty strings; all other values are formatted with %v.
func (e *ExprEngine) Evaluate(exprStr string, pc *PipelineContext) (string, error) {
	env := e.exprEnv(pc)

	program, err := expr.Compile(exprStr,
		expr.Env(env),
		expr.AllowUndefinedVariables(),
	)
	if err != nil {
		return "", fmt.Errorf("expr compile error in %q: %w", exprStr, err)
	}

	result, err := expr.Run(program, env)
	if err != nil {
		return "", fmt.Errorf("expr eval error in %q: %w", exprStr, err)
	}

	if result == nil {
		return "", nil
	}
	return fmt.Sprintf("%v", result), nil
}

// containsExpr reports whether s contains a ${ } expression block.
func containsExpr(s string) bool {
	return strings.Contains(s, "${")
}

// exprBlockRe matches ${ ... } blocks in template strings.
// Expressions must not contain literal } characters (use function calls and
// bracket notation instead of map/set literals).
var exprBlockRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// resolveExprBlocks replaces all ${ ... } blocks in s by evaluating each
// expression against pc. Returns the substituted string or the first error.
func resolveExprBlocks(s string, pc *PipelineContext, ee *ExprEngine) (string, error) {
	var firstErr error
	result := exprBlockRe.ReplaceAllStringFunc(s, func(match string) string {
		if firstErr != nil {
			return match
		}
		inner := strings.TrimSpace(match[2 : len(match)-1]) // strip ${ and }
		val, err := ee.Evaluate(inner, pc)
		if err != nil {
			firstErr = err
			return match
		}
		return val
	})
	return result, firstErr
}
