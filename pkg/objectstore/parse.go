/*
Copyright The FAB Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package objectstore

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
)

// ParseValue converts the text form of a value into the Go value EncodeValue
// expects, using the property's declared type to decide what the text means.
//
// It exists for interfaces that only carry text -- command-line flags, query
// strings, CSV -- and it deliberately does not accept every JSON encoding of
// every type: a caller that can express types should pass typed values instead.
func ParseValue(property Property, text string) (interface{}, error) {
	switch ontologyv1.PropertyType(property.DataType) {
	case ontologyv1.PropertyTypeString,
		ontologyv1.PropertyTypeEnum,
		ontologyv1.PropertyTypeDecimal,
		ontologyv1.PropertyTypeTimestamp,
		ontologyv1.PropertyTypeDate:
		// These are text on the wire and in storage; EncodeValue validates them.
		return text, nil

	case ontologyv1.PropertyTypeBoolean:
		parsed, err := strconv.ParseBool(text)
		if err != nil {
			return nil, parseError(property, text, "true or false")
		}
		return parsed, nil

	case ontologyv1.PropertyTypeInteger, ontologyv1.PropertyTypeLong:
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, parseError(property, text, "a whole number")
		}
		return parsed, nil

	case ontologyv1.PropertyTypeDouble:
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, parseError(property, text, "a number")
		}
		return parsed, nil

	case ontologyv1.PropertyTypeArray:
		return parseArray(property, text)

	case ontologyv1.PropertyTypeJSON:
		var document interface{}
		if err := json.Unmarshal([]byte(text), &document); err != nil {
			return nil, parseError(property, text, "a JSON document")
		}
		return document, nil

	default:
		return nil, fmt.Errorf("property %q: unsupported property type %q: %w",
			property.Name, property.DataType, ErrInvalidValue)
	}
}

// parseArray reads either a JSON array or a comma-separated list, so that the
// common case stays typeable and values containing commas remain expressible.
func parseArray(property Property, text string) (interface{}, error) {
	if strings.HasPrefix(strings.TrimSpace(text), "[") {
		var elements []interface{}
		if err := json.Unmarshal([]byte(text), &elements); err != nil {
			return nil, parseError(property, text, "a JSON array")
		}
		return elements, nil
	}

	itemProperty := Property{
		Name:       property.Name,
		DataType:   property.ItemsType,
		EnumValues: property.EnumValues,
	}
	if strings.TrimSpace(text) == "" {
		return []interface{}{}, nil
	}

	parts := strings.Split(text, ",")
	elements := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		element, err := ParseValue(itemProperty, strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		elements = append(elements, element)
	}
	return elements, nil
}

func parseError(property Property, text, expected string) error {
	return fmt.Errorf("property %q: %q is not %s: %w", property.Name, text, expected, ErrInvalidValue)
}
