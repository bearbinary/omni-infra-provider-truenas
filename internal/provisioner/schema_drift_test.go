package provisioner

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemaProperties parses schema.json and returns the top-level property names.
func schemaProperties(t *testing.T) map[string]any {
	t.Helper()

	data, err := os.ReadFile("../../cmd/omni-infra-provider-truenas/data/schema.json")
	require.NoError(t, err, "schema.json should be readable")

	var schema struct {
		Properties map[string]any `json:"properties"`
	}

	require.NoError(t, json.Unmarshal(data, &schema))
	require.NotEmpty(t, schema.Properties, "schema.json should have properties")

	return schema.Properties
}

// nestedSchemaProperties parses the "items.properties" of an array property in schema.json.
func nestedSchemaProperties(t *testing.T, schemaProps map[string]any, arrayField string) map[string]any {
	t.Helper()

	raw, ok := schemaProps[arrayField]
	require.True(t, ok, "schema.json should have %q property", arrayField)

	b, err := json.Marshal(raw)
	require.NoError(t, err)

	var arrayProp struct {
		Items struct {
			Properties map[string]any `json:"properties"`
		} `json:"items"`
	}

	require.NoError(t, json.Unmarshal(b, &arrayProp))

	return arrayProp.Items.Properties
}

// structYAMLFields returns the yaml tag names for a struct type, excluding omitempty.
func structYAMLFields(t *testing.T, v any) []string {
	t.Helper()

	rt := reflect.TypeOf(v)
	var fields []string

	for i := range rt.NumField() {
		tag := rt.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}

		name := strings.Split(tag, ",")[0]
		fields = append(fields, name)
	}

	sort.Strings(fields)

	return fields
}

// TestSchemaDrift_DataStructMatchesSchema verifies every Data struct field
// has a corresponding schema.json property, and vice versa.
func TestSchemaDrift_DataStructMatchesSchema(t *testing.T) {
	t.Parallel()

	schemaProps := schemaProperties(t)
	structFields := structYAMLFields(t, Data{})

	// Every struct field should be in schema.json
	for _, field := range structFields {
		assert.Contains(t, schemaProps, field,
			"Data struct field %q has no matching schema.json property — add it to schema.json", field)
	}

	// Every schema.json property should be in the struct
	var schemaFields []string
	for k := range schemaProps {
		schemaFields = append(schemaFields, k)
	}

	sort.Strings(schemaFields)

	for _, field := range schemaFields {
		found := false

		for _, sf := range structFields {
			if sf == field {
				found = true

				break
			}
		}

		assert.True(t, found,
			"schema.json property %q has no matching Data struct field — add it to the Data struct or remove from schema.json", field)
	}
}

// TestSchemaDrift_AdditionalDisksMatchesSchema verifies the AdditionalDisk struct
// fields match the additional_disks item properties in schema.json.
func TestSchemaDrift_AdditionalDisksMatchesSchema(t *testing.T) {
	t.Parallel()

	schemaProps := schemaProperties(t)
	itemProps := nestedSchemaProperties(t, schemaProps, "additional_disks")
	structFields := structYAMLFields(t, AdditionalDisk{})

	for _, field := range structFields {
		assert.Contains(t, itemProps, field,
			"AdditionalDisk field %q has no matching schema.json additional_disks item property", field)
	}

	for k := range itemProps {
		found := false

		for _, sf := range structFields {
			if sf == k {
				found = true

				break
			}
		}

		assert.True(t, found,
			"schema.json additional_disks item property %q has no matching AdditionalDisk struct field", k)
	}
}

// TestSchemaDrift_AdditionalNICsMatchesSchema verifies the AdditionalNIC struct
// fields match the additional_nics item properties in schema.json.
func TestSchemaDrift_AdditionalNICsMatchesSchema(t *testing.T) {
	t.Parallel()

	schemaProps := schemaProperties(t)
	itemProps := nestedSchemaProperties(t, schemaProps, "additional_nics")
	structFields := structYAMLFields(t, AdditionalNIC{})

	for _, field := range structFields {
		assert.Contains(t, itemProps, field,
			"AdditionalNIC field %q has no matching schema.json additional_nics item property", field)
	}

	for k := range itemProps {
		found := false

		for _, sf := range structFields {
			if sf == k {
				found = true

				break
			}
		}

		assert.True(t, found,
			"schema.json additional_nics item property %q has no matching AdditionalNIC struct field", k)
	}
}
