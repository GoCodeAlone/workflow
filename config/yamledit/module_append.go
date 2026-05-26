package yamledit

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type GeneratedModule struct {
	Name      string
	Type      string
	Satisfies []string
	Config    map[string]any
	DependsOn []string
}

func AppendGeneratedModules(root *yaml.Node, modules []GeneratedModule) (bool, error) {
	if len(modules) == 0 {
		return false, nil
	}
	doc, err := documentMapping(root)
	if err != nil {
		return false, err
	}
	modulesNode := mappingValue(doc, "modules")
	if modulesNode == nil {
		modulesNode = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		doc.Content = append(doc.Content, scalar("modules"), modulesNode)
	}
	if modulesNode.Kind != yaml.SequenceNode {
		return false, fmt.Errorf("modules must be a YAML sequence")
	}

	existingNames, existingSatisfies := existingModuleIdentity(modulesNode)
	toAppend := filterGeneratedModules(modules, existingNames, existingSatisfies)
	if len(toAppend) == 0 {
		return false, nil
	}

	insertAt := len(modulesNode.Content)
	if lastInfra := lastInfraModuleIndex(modulesNode); lastInfra >= 0 {
		insertAt = lastInfra + 1
	}
	newNodes := make([]*yaml.Node, 0, len(toAppend))
	for i := range toAppend {
		node, err := moduleNode(toAppend[i])
		if err != nil {
			return false, err
		}
		newNodes = append(newNodes, node)
	}
	modulesNode.Content = append(
		modulesNode.Content[:insertAt],
		append(newNodes, modulesNode.Content[insertAt:]...)...,
	)
	return true, nil
}

func documentMapping(root *yaml.Node) (*yaml.Node, error) {
	if root == nil {
		return nil, fmt.Errorf("YAML document is nil")
	}
	switch root.Kind {
	case yaml.DocumentNode:
		if len(root.Content) == 0 {
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
		}
		if root.Content[0].Kind != yaml.MappingNode {
			return nil, fmt.Errorf("YAML document root must be a mapping")
		}
		return root.Content[0], nil
	case yaml.MappingNode:
		return root, nil
	default:
		return nil, fmt.Errorf("YAML document root must be a mapping")
	}
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func existingModuleIdentity(modules *yaml.Node) (map[string]struct{}, map[string]struct{}) {
	names := make(map[string]struct{})
	satisfies := make(map[string]struct{})
	for _, mod := range modules.Content {
		if mod.Kind != yaml.MappingNode {
			continue
		}
		if name := scalarMappingValue(mod, "name"); name != "" {
			names[name] = struct{}{}
		}
		satisfiesNode := mappingValue(mod, "satisfies")
		if satisfiesNode == nil || satisfiesNode.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range satisfiesNode.Content {
			if item.Kind == yaml.ScalarNode && item.Value != "" {
				satisfies[item.Value] = struct{}{}
			}
		}
	}
	return names, satisfies
}

func filterGeneratedModules(modules []GeneratedModule, existingNames, existingSatisfies map[string]struct{}) []GeneratedModule {
	out := make([]GeneratedModule, 0, len(modules))
	seenNames := make(map[string]struct{})
	seenSatisfies := make(map[string]struct{})
	for i := range modules {
		mod := modules[i]
		if _, ok := existingNames[mod.Name]; ok {
			continue
		}
		if _, ok := seenNames[mod.Name]; ok {
			continue
		}
		if anySatisfiesKnown(mod.Satisfies, existingSatisfies) || anySatisfiesKnown(mod.Satisfies, seenSatisfies) {
			continue
		}
		seenNames[mod.Name] = struct{}{}
		for _, key := range mod.Satisfies {
			seenSatisfies[key] = struct{}{}
		}
		out = append(out, mod)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func anySatisfiesKnown(keys []string, known map[string]struct{}) bool {
	for _, key := range keys {
		if _, ok := known[key]; ok {
			return true
		}
	}
	return false
}

func lastInfraModuleIndex(modules *yaml.Node) int {
	last := -1
	for i, mod := range modules.Content {
		if strings.HasPrefix(scalarMappingValue(mod, "type"), "infra.") {
			last = i
		}
	}
	return last
}

func scalarMappingValue(node *yaml.Node, key string) string {
	value := mappingValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

func moduleNode(mod GeneratedModule) (*yaml.Node, error) {
	if mod.Name == "" {
		return nil, fmt.Errorf("generated module name is required")
	}
	if mod.Type == "" {
		return nil, fmt.Errorf("generated module %q type is required", mod.Name)
	}
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendScalarField(node, "name", mod.Name)
	appendScalarField(node, "type", mod.Type)
	if len(mod.Satisfies) > 0 {
		appendSequenceField(node, "satisfies", mod.Satisfies)
	}
	if len(mod.Config) > 0 {
		configNode, err := valueNode(mod.Config)
		if err != nil {
			return nil, fmt.Errorf("generated module %q config: %w", mod.Name, err)
		}
		node.Content = append(node.Content, scalar("config"), configNode)
	}
	if len(mod.DependsOn) > 0 {
		appendSequenceField(node, "dependsOn", mod.DependsOn)
	}
	return node, nil
}

func appendScalarField(node *yaml.Node, key, value string) {
	node.Content = append(node.Content, scalar(key), scalar(value))
}

func appendSequenceField(node *yaml.Node, key string, values []string) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, value := range values {
		seq.Content = append(seq.Content, scalar(value))
	}
	node.Content = append(node.Content, scalar(key), seq)
}

func valueNode(value any) (*yaml.Node, error) {
	switch typed := value.(type) {
	case map[string]any:
		return mapNode(typed)
	case map[string]string:
		asAny := make(map[string]any, len(typed))
		for k, v := range typed {
			asAny[k] = v
		}
		return mapNode(asAny)
	case []any:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range typed {
			child, err := valueNode(item)
			if err != nil {
				return nil, err
			}
			seq.Content = append(seq.Content, child)
		}
		return seq, nil
	case []string:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range typed {
			seq.Content = append(seq.Content, scalar(item))
		}
		return seq, nil
	default:
		var node yaml.Node
		if err := node.Encode(value); err != nil {
			return nil, err
		}
		return &node, nil
	}
}

func mapNode(values map[string]any) (*yaml.Node, error) {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		child, err := valueNode(values[key])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		node.Content = append(node.Content, scalar(key), child)
	}
	return node, nil
}

func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}
