package dynamic

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/GoCodeAlone/yaegi/interp"
)

// ComponentStatus describes the lifecycle state of a dynamic component.
type ComponentStatus string

const (
	StatusUnloaded    ComponentStatus = "unloaded"
	StatusLoaded      ComponentStatus = "loaded"
	StatusInitialized ComponentStatus = "initialized"
	StatusRunning     ComponentStatus = "running"
	StatusStopped     ComponentStatus = "stopped"
	StatusError       ComponentStatus = "error"
)

// ComponentInfo holds metadata about a loaded dynamic component.
type ComponentInfo struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Source   string          `json:"source,omitempty"`
	Status   ComponentStatus `json:"status"`
	LoadedAt time.Time       `json:"loaded_at"`
	Error    string          `json:"error,omitempty"`
}

// DynamicComponent wraps Yaegi-interpreted Go code as a workflow component
// that satisfies the modular.Module interface.
type DynamicComponent struct {
	mu     sync.RWMutex
	id     string
	source string
	info   ComponentInfo

	pool        *InterpreterPool
	interpreter *interp.Interpreter

	// Extracted function references from interpreted code
	nameFunc     func() string
	initFunc     func(map[string]any) error
	startFunc    func(context.Context) error
	stopFunc     func(context.Context) error
	executeFunc  func(context.Context, map[string]any) (map[string]any, error)
	contractFunc func() *FieldContract

	// Contract holds the field contract extracted from the component, if declared.
	Contract *FieldContract
}

// NewDynamicComponent creates a new unloaded dynamic component.
func NewDynamicComponent(id string, pool *InterpreterPool) *DynamicComponent {
	return &DynamicComponent{
		id:   id,
		pool: pool,
		info: ComponentInfo{
			ID:     id,
			Status: StatusUnloaded,
		},
	}
}

// Name returns the component name. If interpreted code provides a Name()
// function, that value is used; otherwise the component ID is returned.
func (dc *DynamicComponent) Name() string {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	if dc.nameFunc != nil {
		return dc.safeCallName()
	}
	return dc.id
}

// Init satisfies modular.Module. It delegates to the interpreted Init function.
func (dc *DynamicComponent) Init(services map[string]any) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if dc.initFunc != nil {
		if err := dc.safeCallInit(services); err != nil {
			dc.info.Status = StatusError
			dc.info.Error = err.Error()
			return err
		}
	}
	dc.info.Status = StatusInitialized
	return nil
}

// Start runs the interpreted Start function.
func (dc *DynamicComponent) Start(ctx context.Context) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if dc.startFunc != nil {
		if err := dc.safeCallStart(ctx); err != nil {
			dc.info.Status = StatusError
			dc.info.Error = err.Error()
			return err
		}
	}
	dc.info.Status = StatusRunning
	return nil
}

// Stop runs the interpreted Stop function.
func (dc *DynamicComponent) Stop(ctx context.Context) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if dc.stopFunc != nil {
		if err := dc.safeCallStop(ctx); err != nil {
			dc.info.Status = StatusError
			dc.info.Error = err.Error()
			return err
		}
	}
	dc.info.Status = StatusStopped
	return nil
}

// Execute runs the interpreted Execute function. If the component declares a
// field contract, inputs are validated and defaults applied before execution.
func (dc *DynamicComponent) Execute(ctx context.Context, params map[string]any) (map[string]any, error) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	if dc.executeFunc == nil {
		return nil, fmt.Errorf("component %q has no Execute function", dc.id)
	}
	if dc.Contract != nil {
		if err := ValidateInputs(dc.Contract, params); err != nil {
			return nil, fmt.Errorf("component %q: %w", dc.id, err)
		}
		params = ApplyDefaults(dc.Contract, params)
	}
	return dc.safeCallExecute(ctx, params)
}

// LoadFromSource compiles and loads Go source code into the component.
func (dc *DynamicComponent) LoadFromSource(source string) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	i, err := dc.pool.NewInterpreter()
	if err != nil {
		return fmt.Errorf("failed to create interpreter: %w", err)
	}

	// Evaluate the source
	if _, err := i.Eval(source); err != nil {
		dc.info.Status = StatusError
		dc.info.Error = err.Error()
		return fmt.Errorf("failed to evaluate source: %w", err)
	}

	dc.interpreter = i
	dc.source = source
	dc.info.Source = source

	// Extract known function symbols from interpreted code.
	dc.extractFunctions(i)

	dc.info.Status = StatusLoaded
	dc.info.LoadedAt = time.Now()
	dc.info.Name = dc.id
	if dc.nameFunc != nil {
		dc.info.Name = dc.safeCallName()
	}
	dc.info.Error = ""

	// Extract field contract if the component declares one
	if dc.contractFunc != nil {
		dc.Contract = dc.safeCallContract()
	}

	return nil
}

// Info returns the current component metadata.
func (dc *DynamicComponent) Info() ComponentInfo {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return dc.info
}

// Source returns the loaded source code.
func (dc *DynamicComponent) Source() string {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return dc.source
}

// extractFunctions looks up well-known function symbols in the interpreter
// and binds them as typed Go function references.
func (dc *DynamicComponent) extractFunctions(i *interp.Interpreter) {
	// Try to extract Name() string
	if v, err := i.Eval("component.Name"); err == nil {
		if fn, ok := v.Interface().(func() string); ok {
			dc.nameFunc = fn
		}
	}

	// Try to extract Init(map[string]interface{}) error
	if v, err := i.Eval("component.Init"); err == nil {
		if fn, ok := v.Interface().(func(map[string]any) error); ok {
			dc.initFunc = fn
		}
	}

	// Try to extract Start(context.Context) error
	if v, err := i.Eval("component.Start"); err == nil {
		if fn, ok := v.Interface().(func(context.Context) error); ok {
			dc.startFunc = fn
		}
	}

	// Try to extract Stop(context.Context) error
	if v, err := i.Eval("component.Stop"); err == nil {
		if fn, ok := v.Interface().(func(context.Context) error); ok {
			dc.stopFunc = fn
		}
	}

	// Try to extract Execute(context.Context, map[string]interface{}) (map[string]interface{}, error)
	if v, err := i.Eval("component.Execute"); err == nil {
		fn := v.Interface()
		// The Yaegi interpreter may return the function with concrete types
		// that match the signature but not as the exact Go type. Use reflection
		// to adapt.
		if execFn, ok := fn.(func(context.Context, map[string]any) (map[string]any, error)); ok {
			dc.executeFunc = execFn
		} else {
			dc.executeFunc = dc.makeExecuteAdapter(v)
		}
	}

	// Try to extract Contract() *FieldContract
	// Dynamic components use map-based contracts to avoid importing the dynamic package.
	// The convention is: Contract() map[string]interface{} with keys
	// "required_inputs", "optional_inputs", "outputs", each a map[string]interface{}
	// where each value is a map with "type", "description", "default".
	if v, err := i.Eval("component.Contract"); err == nil {
		if fn, ok := v.Interface().(func() map[string]any); ok {
			dc.contractFunc = func() *FieldContract {
				return parseContractMap(fn())
			}
		}
	}
}

// makeExecuteAdapter uses reflection to create an Execute adapter when the
// Yaegi-returned function doesn't directly type-assert.
func (dc *DynamicComponent) makeExecuteAdapter(v reflect.Value) func(context.Context, map[string]any) (map[string]any, error) {
	if !v.IsValid() || v.Kind() != reflect.Func {
		return nil
	}
	return func(ctx context.Context, params map[string]any) (map[string]any, error) {
		results := v.Call([]reflect.Value{
			reflect.ValueOf(ctx),
			reflect.ValueOf(params),
		})
		if len(results) != 2 {
			return nil, fmt.Errorf("Execute returned %d values, expected 2", len(results))
		}
		var res map[string]any
		if !results[0].IsNil() {
			res = results[0].Interface().(map[string]any)
		}
		var err error
		if !results[1].IsNil() {
			err = results[1].Interface().(error)
		}
		return res, err
	}
}

// Safe call wrappers that recover from panics in interpreted code.

func (dc *DynamicComponent) safeCallName() (name string) {
	defer func() {
		if r := recover(); r != nil {
			name = dc.id
		}
	}()
	return dc.nameFunc()
}

func (dc *DynamicComponent) safeCallInit(services map[string]any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in Init: %v", r)
		}
	}()
	return dc.initFunc(services)
}

func (dc *DynamicComponent) safeCallStart(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in Start: %v", r)
		}
	}()
	return dc.startFunc(ctx)
}

func (dc *DynamicComponent) safeCallStop(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in Stop: %v", r)
		}
	}()
	return dc.stopFunc(ctx)
}

func (dc *DynamicComponent) safeCallExecute(ctx context.Context, params map[string]any) (result map[string]any, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = fmt.Errorf("panic in Execute: %v", r)
		}
	}()
	return dc.executeFunc(ctx, params)
}

func (dc *DynamicComponent) safeCallContract() (contract *FieldContract) {
	defer func() {
		if r := recover(); r != nil {
			contract = nil
		}
	}()
	return dc.contractFunc()
}

// parseContractMap converts a map[string]any returned by a dynamic component's
// Contract() function into a typed FieldContract. The expected structure is:
//
//	{
//	  "required_inputs": { "fieldName": {"type": "string", "description": "..."} },
//	  "optional_inputs": { "fieldName": {"type": "int", "description": "...", "default": 0} },
//	  "outputs":         { "fieldName": {"type": "string", "description": "..."} },
//	}
func parseContractMap(m map[string]any) *FieldContract {
	if m == nil {
		return nil
	}
	c := NewFieldContract()
	if ri, ok := m["required_inputs"].(map[string]any); ok {
		c.RequiredInputs = parseFieldSpecs(ri)
	}
	if oi, ok := m["optional_inputs"].(map[string]any); ok {
		c.OptionalInputs = parseFieldSpecs(oi)
	}
	if out, ok := m["outputs"].(map[string]any); ok {
		c.Outputs = parseFieldSpecs(out)
	}
	return c
}

func parseFieldSpecs(m map[string]any) map[string]FieldSpec {
	specs := make(map[string]FieldSpec, len(m))
	for name, val := range m {
		specMap, ok := val.(map[string]any)
		if !ok {
			// Simple form: just the type string
			if ts, ok := val.(string); ok {
				specs[name] = FieldSpec{Type: FieldType(ts)}
			}
			continue
		}
		spec := FieldSpec{}
		if t, ok := specMap["type"].(string); ok {
			spec.Type = FieldType(t)
		}
		if d, ok := specMap["description"].(string); ok {
			spec.Description = d
		}
		if def, ok := specMap["default"]; ok {
			spec.Default = def
		}
		specs[name] = spec
	}
	return specs
}
