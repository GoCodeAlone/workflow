package sdk

import (
	"encoding/json"
	"fmt"
	"strings"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// MessageContract describes a descriptor-only protobuf message contract.
type MessageContract struct {
	ContractType    string   `json:"contractType"`
	ProtoPackage    string   `json:"protoPackage"`
	MessageNames    []string `json:"messageNames"`
	GoImportPath    string   `json:"goImportPath,omitempty"`
	SchemaDigest    string   `json:"schemaDigest,omitempty"`
	ProtocolVersion string   `json:"protocolVersion,omitempty"`
}

// ContractDescriptor converts c into the shared plugin contract descriptor.
func (c MessageContract) ContractDescriptor() (*pb.ContractDescriptor, error) {
	if strings.TrimSpace(c.ContractType) == "" {
		return nil, fmt.Errorf("message contract type is required")
	}
	if strings.TrimSpace(c.ProtoPackage) == "" {
		return nil, fmt.Errorf("message contract proto package is required")
	}
	if len(c.MessageNames) == 0 {
		return nil, fmt.Errorf("message contract must declare at least one message")
	}
	if strings.TrimSpace(c.SchemaDigest) == "" {
		return nil, fmt.Errorf("message contract schema digest is required")
	}
	if strings.TrimSpace(c.ProtocolVersion) == "" {
		return nil, fmt.Errorf("message contract protocol version is required")
	}
	names := make([]string, 0, len(c.MessageNames))
	for _, name := range c.MessageNames {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("message contract contains empty message name")
		}
		names = append(names, name)
	}
	return &pb.ContractDescriptor{
		Kind:            pb.ContractKind_CONTRACT_KIND_MESSAGE,
		ContractType:    c.ContractType,
		ProtoPackage:    c.ProtoPackage,
		MessageNames:    names,
		GoImportPath:    c.GoImportPath,
		SchemaDigest:    c.SchemaDigest,
		ProtocolVersion: c.ProtocolVersion,
		Mode:            pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
	}, nil
}

func (c MessageContract) pluginContractsJSONMap() (map[string]any, error) {
	if _, err := c.ContractDescriptor(); err != nil {
		return nil, err
	}
	out := map[string]any{
		"kind":            "message",
		"contractType":    c.ContractType,
		"protoPackage":    c.ProtoPackage,
		"messageNames":    append([]string(nil), c.MessageNames...),
		"mode":            "strict",
		"goImportPath":    c.GoImportPath,
		"schemaDigest":    c.SchemaDigest,
		"protocolVersion": c.ProtocolVersion,
	}
	return out, nil
}

func encodePluginContractsJSON(contracts []map[string]any) string {
	payload := map[string]any{
		"version":   "v1",
		"contracts": contracts,
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data) + "\n"
}
