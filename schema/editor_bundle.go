package schema

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	EditorBundleSchemaVersion = "editor-bundle/v1"

	EditorContractSourceBuiltin             = "builtin"
	EditorContractSourcePluginManifest      = "plugin-manifest"
	EditorContractSourcePluginContractsJSON = "plugin-contracts-json"
	EditorContractSourceLivePlugin          = "live-plugin"
)

type EditorBundleOptions struct {
	WorkflowVersion    string
	ContractRegistries []EditorContractRegistrySource
}

type EditorContractRegistrySource struct {
	Plugin                    string
	Source                    string
	Registry                  *pb.ContractRegistry
	DescriptorSetRef          string
	ContractDescriptorSetRefs map[string]string
}

type EditorContractBundle struct {
	Version         string                               `json:"version"`
	WorkflowVersion string                               `json:"workflowVersion"`
	ModuleSchemas   map[string]*ModuleSchema             `json:"moduleSchemas"`
	StepSchemas     map[string]*StepSchema               `json:"stepSchemas"`
	CoercionRules   map[string][]string                  `json:"coercionRules"`
	Contracts       map[string]*EditorContractDescriptor `json:"contracts"`
	Messages        map[string]*EditorMessageDescriptor  `json:"messages"`
	DescriptorSets  map[string]*EditorDescriptorSet      `json:"descriptorSets"`
	DSLReference    any                                  `json:"dslReference,omitempty"`
	Schemas         EditorYAMLSchemas                    `json:"schemas"`
	Snippets        []EditorSnippet                      `json:"snippets"`
}

type EditorContractDescriptor struct {
	ID               string `json:"id"`
	Plugin           string `json:"plugin,omitempty"`
	OwnerType        string `json:"ownerType"`
	OwnerKey         string `json:"ownerKey"`
	Mode             string `json:"mode"`
	RequestMessage   string `json:"requestMessage,omitempty"`
	ResponseMessage  string `json:"responseMessage,omitempty"`
	ConfigMessage    string `json:"configMessage,omitempty"`
	DescriptorSetRef string `json:"descriptorSetRef,omitempty"`
	Source           string `json:"source"`
}

type EditorMessageDescriptor struct {
	Name             string               `json:"name"`
	FullName         string               `json:"fullName"`
	Fields           []EditorMessageField `json:"fields"`
	DescriptorSetRef string               `json:"descriptorSetRef,omitempty"`
}

type EditorMessageField struct {
	Name        string `json:"name"`
	JSONName    string `json:"jsonName,omitempty"`
	Number      int32  `json:"number"`
	Type        string `json:"type"`
	MessageType string `json:"messageType,omitempty"`
	EnumType    string `json:"enumType,omitempty"`
	Repeated    bool   `json:"repeated,omitempty"`
	Map         bool   `json:"map,omitempty"`
}

type EditorDescriptorSet struct {
	ID              string   `json:"id"`
	Plugin          string   `json:"plugin,omitempty"`
	Encoding        string   `json:"encoding"`
	SHA256          string   `json:"sha256,omitempty"`
	EmbeddedBase64  string   `json:"embeddedBase64,omitempty"`
	FileCount       int      `json:"fileCount"`
	MessageCount    int      `json:"messageCount"`
	MessageNames    []string `json:"messageNames"`
	ExternalRef     string   `json:"externalRef,omitempty"`
	ExternalRefType string   `json:"externalRefType,omitempty"`
}

type EditorYAMLSchemas struct {
	App   *Schema `json:"app"`
	Infra *Schema `json:"infra,omitempty"`
	Wfctl *Schema `json:"wfctl,omitempty"`
}

type EditorSnippet struct {
	Name        string   `json:"name"`
	Prefix      string   `json:"prefix"`
	Description string   `json:"description,omitempty"`
	Body        []string `json:"body"`
}

func ExportEditorBundle(opts EditorBundleOptions) (*EditorContractBundle, error) {
	moduleReg := NewModuleSchemaRegistry()
	stepReg := NewStepSchemaRegistry()
	coercionReg := NewTypeCoercionRegistry()

	bundle := &EditorContractBundle{
		Version:         EditorBundleSchemaVersion,
		WorkflowVersion: opts.WorkflowVersion,
		ModuleSchemas:   moduleReg.AllMap(),
		StepSchemas:     stepReg.AllMap(),
		CoercionRules:   coercionReg.Rules(),
		Contracts:       map[string]*EditorContractDescriptor{},
		Messages:        map[string]*EditorMessageDescriptor{},
		DescriptorSets:  map[string]*EditorDescriptorSet{},
		Schemas: EditorYAMLSchemas{
			App:   GenerateWorkflowSchema(),
			Infra: GenerateInfraSchema(),
			Wfctl: GenerateWfctlSchema(),
		},
		Snippets: editorSnippets(),
	}

	for _, source := range opts.ContractRegistries {
		if err := addContractRegistryToBundle(bundle, source); err != nil {
			return nil, err
		}
	}
	return bundle, nil
}

func editorSnippets() []EditorSnippet {
	snippets := GetSnippets()
	out := make([]EditorSnippet, 0, len(snippets))
	for _, snippet := range snippets {
		body := make([]string, len(snippet.Body))
		copy(body, snippet.Body)
		out = append(out, EditorSnippet{
			Name:        snippet.Name,
			Prefix:      snippet.Prefix,
			Description: snippet.Description,
			Body:        body,
		})
	}
	return out
}

func addContractRegistryToBundle(bundle *EditorContractBundle, source EditorContractRegistrySource) error {
	if source.Registry == nil {
		return nil
	}
	descriptorSetRef := source.DescriptorSetRef
	if source.Registry.FileDescriptorSet != nil && len(source.Registry.FileDescriptorSet.File) > 0 {
		descriptorSet, err := normalizeDescriptorSet(source.Plugin, source.Registry.FileDescriptorSet)
		if err != nil {
			return err
		}
		bundle.DescriptorSets[descriptorSet.ID] = descriptorSet
		descriptorSetRef = descriptorSet.ID
		if err := addMessagesFromDescriptorSet(bundle.Messages, descriptorSetRef, source.Registry.FileDescriptorSet); err != nil {
			return err
		}
	} else if descriptorSetRef != "" {
		bundle.DescriptorSets[descriptorSetRef] = &EditorDescriptorSet{
			ID:              descriptorSetRef,
			Plugin:          source.Plugin,
			Encoding:        "external",
			ExternalRef:     descriptorSetRef,
			ExternalRefType: "path",
		}
	}
	for _, descriptor := range source.Registry.Contracts {
		contractDescriptorSetRef := descriptorSetRefForContract(source, descriptor, descriptorSetRef)
		if contractDescriptorSetRef != "" && bundle.DescriptorSets[contractDescriptorSetRef] == nil {
			bundle.DescriptorSets[contractDescriptorSetRef] = &EditorDescriptorSet{
				ID:              contractDescriptorSetRef,
				Plugin:          source.Plugin,
				Encoding:        "external",
				ExternalRef:     contractDescriptorSetRef,
				ExternalRefType: "path",
			}
		}
		contract := normalizeContractDescriptor(source, descriptor, contractDescriptorSetRef)
		if contract == nil {
			continue
		}
		bundle.Contracts[contract.ID] = contract
		addReferencedMessagePlaceholders(bundle.Messages, contract, contractDescriptorSetRef)
	}
	return nil
}

func descriptorSetRefForContract(source EditorContractRegistrySource, descriptor *pb.ContractDescriptor, fallback string) string {
	if len(source.ContractDescriptorSetRefs) == 0 {
		return fallback
	}
	ownerType, ownerKey := editorContractOwner(descriptor)
	if ownerType == "" || ownerKey == "" {
		return fallback
	}
	id := ownerType + ":" + ownerKey
	if ownerType == "service" && descriptor.GetServiceName() != "" {
		id = "service:" + descriptor.GetServiceName() + "/" + descriptor.GetMethod()
	}
	if ref := source.ContractDescriptorSetRefs[id]; ref != "" {
		return ref
	}
	return fallback
}

func normalizeDescriptorSet(plugin string, set *descriptorpb.FileDescriptorSet) (*EditorDescriptorSet, error) {
	data, err := proto.Marshal(set)
	if err != nil {
		return nil, fmt.Errorf("marshal descriptor set: %w", err)
	}
	sum := sha256.Sum256(data)
	idParts := []string{"descriptor-set"}
	if plugin != "" {
		idParts = append(idParts, plugin)
	}
	idParts = append(idParts, hex.EncodeToString(sum[:8]))

	names := descriptorSetMessageNames(set)
	return &EditorDescriptorSet{
		ID:             strings.Join(idParts, ":"),
		Plugin:         plugin,
		Encoding:       "protobuf.FileDescriptorSet+base64",
		SHA256:         hex.EncodeToString(sum[:]),
		EmbeddedBase64: base64.StdEncoding.EncodeToString(data),
		FileCount:      len(set.File),
		MessageCount:   len(names),
		MessageNames:   names,
	}, nil
}

func descriptorSetMessageNames(set *descriptorpb.FileDescriptorSet) []string {
	var names []string
	for _, file := range set.File {
		pkg := file.GetPackage()
		for _, msg := range file.MessageType {
			collectDescriptorProtoNames(&names, pkg, "", msg)
		}
	}
	sort.Strings(names)
	return names
}

func collectDescriptorProtoNames(names *[]string, pkg, parent string, msg *descriptorpb.DescriptorProto) {
	if msg == nil {
		return
	}
	name := msg.GetName()
	if parent != "" {
		name = parent + "." + name
	}
	fullName := name
	if pkg != "" {
		fullName = pkg + "." + name
	}
	*names = append(*names, fullName)
	for _, nested := range msg.NestedType {
		collectDescriptorProtoNames(names, pkg, name, nested)
	}
}

func addMessagesFromDescriptorSet(messages map[string]*EditorMessageDescriptor, descriptorSetRef string, set *descriptorpb.FileDescriptorSet) error {
	files, err := protodesc.NewFiles(set)
	if err != nil {
		return fmt.Errorf("parse descriptor set: %w", err)
	}
	var walkErr error
	files.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		walkErr = addMessageDescriptors(messages, descriptorSetRef, file.Messages())
		return walkErr == nil
	})
	return walkErr
}

func addMessageDescriptors(messages map[string]*EditorMessageDescriptor, descriptorSetRef string, descriptors protoreflect.MessageDescriptors) error {
	for i := 0; i < descriptors.Len(); i++ {
		descriptor := descriptors.Get(i)
		fullName := string(descriptor.FullName())
		messages[fullName] = &EditorMessageDescriptor{
			Name:             string(descriptor.Name()),
			FullName:         fullName,
			Fields:           editorMessageFields(descriptor.Fields()),
			DescriptorSetRef: descriptorSetRef,
		}
		if err := addMessageDescriptors(messages, descriptorSetRef, descriptor.Messages()); err != nil {
			return err
		}
	}
	return nil
}

func editorMessageFields(fields protoreflect.FieldDescriptors) []EditorMessageField {
	out := make([]EditorMessageField, 0, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		editorField := EditorMessageField{
			Name:     string(field.Name()),
			JSONName: field.JSONName(),
			Number:   int32(field.Number()),
			Type:     editorFieldType(field),
			Repeated: field.IsList(),
			Map:      field.IsMap(),
		}
		if field.Message() != nil {
			editorField.MessageType = string(field.Message().FullName())
		}
		if field.Enum() != nil {
			editorField.EnumType = string(field.Enum().FullName())
		}
		out = append(out, editorField)
	}
	return out
}

func editorFieldType(field protoreflect.FieldDescriptor) string {
	if field.IsMap() {
		return "map"
	}
	switch field.Kind() {
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.EnumKind:
		return "enum"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "int32"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "uint32"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "int64"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "uint64"
	case protoreflect.FloatKind:
		return "float"
	case protoreflect.DoubleKind:
		return "double"
	case protoreflect.StringKind:
		return "string"
	case protoreflect.BytesKind:
		return "bytes"
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return "message"
	default:
		return string(field.Kind().String())
	}
}

func normalizeContractDescriptor(source EditorContractRegistrySource, descriptor *pb.ContractDescriptor, descriptorSetRef string) *EditorContractDescriptor {
	if descriptor == nil {
		return nil
	}
	ownerType, ownerKey := editorContractOwner(descriptor)
	if ownerType == "" || ownerKey == "" {
		return nil
	}
	id := ownerType + ":" + ownerKey
	if ownerType == "service" && descriptor.ServiceName != "" {
		id = "service:" + descriptor.ServiceName + "/" + descriptor.Method
	}
	return &EditorContractDescriptor{
		ID:               id,
		Plugin:           source.Plugin,
		OwnerType:        ownerType,
		OwnerKey:         ownerKey,
		Mode:             editorContractMode(descriptor.Mode),
		RequestMessage:   descriptor.InputMessage,
		ResponseMessage:  descriptor.OutputMessage,
		ConfigMessage:    descriptor.ConfigMessage,
		DescriptorSetRef: descriptorSetRef,
		Source:           editorContractSource(source.Source),
	}
}

func editorContractOwner(descriptor *pb.ContractDescriptor) (string, string) {
	switch descriptor.Kind {
	case pb.ContractKind_CONTRACT_KIND_MODULE:
		return "module", descriptor.ModuleType
	case pb.ContractKind_CONTRACT_KIND_STEP:
		return "step", descriptor.StepType
	case pb.ContractKind_CONTRACT_KIND_TRIGGER:
		return "trigger", descriptor.TriggerType
	case pb.ContractKind_CONTRACT_KIND_SERVICE:
		if descriptor.ServiceName == "" {
			return "service", descriptor.Method
		}
		return "service", descriptor.ServiceName + "/" + descriptor.Method
	default:
		return "", ""
	}
}

func editorContractMode(mode pb.ContractMode) string {
	switch mode {
	case pb.ContractMode_CONTRACT_MODE_STRICT_PROTO:
		return "strict"
	case pb.ContractMode_CONTRACT_MODE_PROTO_WITH_LEGACY_STRUCT:
		return "proto_with_legacy"
	case pb.ContractMode_CONTRACT_MODE_LEGACY_STRUCT:
		return "legacy"
	default:
		return "unspecified"
	}
}

func editorContractSource(source string) string {
	switch source {
	case EditorContractSourceBuiltin,
		EditorContractSourcePluginManifest,
		EditorContractSourcePluginContractsJSON,
		EditorContractSourceLivePlugin:
		return source
	default:
		return EditorContractSourceBuiltin
	}
}

func addReferencedMessagePlaceholders(messages map[string]*EditorMessageDescriptor, contract *EditorContractDescriptor, descriptorSetRef string) {
	for _, name := range []string{contract.ConfigMessage, contract.RequestMessage, contract.ResponseMessage} {
		if name == "" {
			continue
		}
		if _, ok := messages[name]; ok {
			continue
		}
		shortName := name
		if idx := strings.LastIndex(shortName, "."); idx >= 0 {
			shortName = shortName[idx+1:]
		}
		messages[name] = &EditorMessageDescriptor{
			Name:             shortName,
			FullName:         name,
			DescriptorSetRef: descriptorSetRef,
		}
	}
}

func GenerateInfraSchema() *Schema {
	s := &Schema{
		Schema:      "https://json-schema.org/draft/2020-12/schema",
		Title:       "Workflow infrastructure configuration",
		Description: "Schema for infra.yaml files consumed by wfctl infrastructure workflows.",
		Type:        "object",
		Properties: map[string]*Schema{
			"infrastructure": {Type: "object"},
			"providers":      {Type: "object"},
			"environments":   {Type: "object"},
			"sidecars":       {Type: "array", Items: &Schema{Type: "object"}},
			"state":          {Type: "object"},
		},
	}
	s.setAdditionalPropertiesBool(true)
	return s
}

func GenerateWfctlSchema() *Schema {
	s := &Schema{
		Schema:      "https://json-schema.org/draft/2020-12/schema",
		Title:       "wfctl project configuration",
		Description: "Schema for wfctl.yaml project metadata and tool policy.",
		Type:        "object",
		Properties: map[string]*Schema{
			"project":      {Type: "object"},
			"plugins":      {Type: "object"},
			"registries":   {Type: "array", Items: &Schema{Type: "object"}},
			"environments": {Type: "object"},
			"validation":   {Type: "object"},
			"deploy":       {Type: "object"},
			"release":      {Type: "object"},
			"secrets":      {Type: "object"},
		},
	}
	s.setAdditionalPropertiesBool(true)
	return s
}
