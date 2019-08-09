package validation

import (
	"github.com/ghodss/yaml"
	"github.com/go-openapi/spec"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/validate"
)

type Schema interface {
	GetMissingEntries(crInstance interface{}) []SchemaEntry
	Validate(data interface{}) error
}

func New(crd []byte) (Schema, error) {
	object := &customResourceDefinition{}
	err := yaml.Unmarshal(crd, object)
	if err != nil {
		return nil, err
	}
	return &openAPIV3Schema{&object.Spec.Validation.OpenAPIV3Schema}, nil
}

type openAPIV3Schema struct {
	schema *spec.Schema
}

func (schema *openAPIV3Schema) GetMissingEntries(crInstance interface{}) []SchemaEntry {
	return getMissingEntries(schema.schema, crInstance)
}

func (schema *openAPIV3Schema) Validate(data interface{}) error {
	return validate.AgainstSchema(schema.schema, data, strfmt.Default)
}

type customResourceDefinition struct {
	Spec customResourceDefinitionSpec `json:"spec,omitempty"`
}

type customResourceDefinitionSpec struct {
	Validation customResourceDefinitionValidation `json:"validation,omitempty"`
}

type customResourceDefinitionValidation struct {
	OpenAPIV3Schema spec.Schema `json:"openAPIV3Schema,omitempty"`
}
