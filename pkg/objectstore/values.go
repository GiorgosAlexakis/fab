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
	"math"
	"strconv"
	"strings"
	"time"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
)

// DateLayout is the wire and storage format of a date property.
const DateLayout = "2006-01-02"

// EncodeValue converts a Go value into the JSON stored in object_props.value.
//
// Values are checked against the property's declared type here, on the way in.
// A generic property table has no column type to lean on, so this function is
// the only thing standing between a typed ontology and a bag of arbitrary JSON.
func EncodeValue(property Property, value interface{}) ([]byte, error) {
	if value == nil {
		return nil, fmt.Errorf("property %q: a nil value must be removed rather than written: %w",
			property.Name, ErrInvalidValue)
	}

	normalized, err := normalizeValue(property, ontologyv1.PropertyType(property.DataType), value)
	if err != nil {
		return nil, err
	}

	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("property %q: encoding value: %w", property.Name, err)
	}
	return encoded, nil
}

// DecodeValue converts a stored value back into a Go value. Nullable properties
// with no row are absent from an object's map, so a nil payload here means the
// value was explicitly stored as JSON null.
func DecodeValue(property Property, raw []byte) (interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("property %q: decoding stored value: %w", property.Name, err)
	}
	if decoded == nil {
		return nil, nil
	}

	return decodeTyped(property, ontologyv1.PropertyType(property.DataType), decoded)
}

// UniqueKey returns the text form of a value used to enforce the uniqueness
// declared in the ontology. Values that are equal must produce the same key, so
// numbers are canonicalised: 1 and 1.0 are one value, not two.
func UniqueKey(property Property, value interface{}) (string, error) {
	normalized, err := normalizeValue(property, ontologyv1.PropertyType(property.DataType), value)
	if err != nil {
		return "", err
	}

	switch typed := normalized.(type) {
	case string:
		return typed, nil
	case bool:
		return strconv.FormatBool(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("property %q: %s values cannot be unique: %w",
			property.Name, property.DataType, ErrInvalidValue)
	}
}

// normalizeValue validates value against dataType and returns the canonical Go
// value that is marshalled into storage.
func normalizeValue(property Property, dataType ontologyv1.PropertyType, value interface{}) (interface{}, error) {
	switch dataType {
	case ontologyv1.PropertyTypeString:
		return asString(property, value)

	case ontologyv1.PropertyTypeBoolean:
		asBool, ok := value.(bool)
		if !ok {
			return nil, typeError(property, dataType, value)
		}
		return asBool, nil

	case ontologyv1.PropertyTypeInteger:
		integer, err := asInt64(property, dataType, value)
		if err != nil {
			return nil, err
		}
		if integer < math.MinInt32 || integer > math.MaxInt32 {
			return nil, fmt.Errorf("property %q: %d is out of range for integer, use long: %w",
				property.Name, integer, ErrInvalidValue)
		}
		return integer, nil

	case ontologyv1.PropertyTypeLong:
		return asInt64(property, dataType, value)

	case ontologyv1.PropertyTypeDouble:
		return asFloat64(property, dataType, value)

	case ontologyv1.PropertyTypeDecimal:
		return asDecimal(property, value)

	case ontologyv1.PropertyTypeTimestamp:
		return asTimestamp(property, value)

	case ontologyv1.PropertyTypeDate:
		return asDate(property, value)

	case ontologyv1.PropertyTypeEnum:
		text, err := asString(property, value)
		if err != nil {
			return nil, err
		}
		for _, permitted := range property.EnumValues {
			if text == permitted {
				return text, nil
			}
		}
		return nil, fmt.Errorf("property %q: %q is not one of %s: %w",
			property.Name, text, strings.Join(property.EnumValues, ", "), ErrInvalidValue)

	case ontologyv1.PropertyTypeArray:
		return asArray(property, value)

	case ontologyv1.PropertyTypeJSON:
		// An opaque document: anything that round-trips through JSON is valid.
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("property %q: value is not JSON-encodable: %w", property.Name, ErrInvalidValue)
		}
		var reparsed interface{}
		if err := json.Unmarshal(encoded, &reparsed); err != nil {
			return nil, fmt.Errorf("property %q: value is not JSON-encodable: %w", property.Name, ErrInvalidValue)
		}
		return reparsed, nil

	default:
		return nil, fmt.Errorf("property %q: unsupported property type %q: %w",
			property.Name, dataType, ErrInvalidValue)
	}
}

// decodeTyped converts the JSON-decoded value into the Go type callers expect
// for the property's declared type.
func decodeTyped(property Property, dataType ontologyv1.PropertyType, decoded interface{}) (interface{}, error) {
	switch dataType {
	case ontologyv1.PropertyTypeString, ontologyv1.PropertyTypeEnum, ontologyv1.PropertyTypeDecimal:
		text, ok := decoded.(string)
		if !ok {
			return nil, storedTypeError(property, dataType, decoded)
		}
		return text, nil

	case ontologyv1.PropertyTypeBoolean:
		asBool, ok := decoded.(bool)
		if !ok {
			return nil, storedTypeError(property, dataType, decoded)
		}
		return asBool, nil

	case ontologyv1.PropertyTypeInteger, ontologyv1.PropertyTypeLong:
		number, ok := decoded.(float64)
		if !ok {
			return nil, storedTypeError(property, dataType, decoded)
		}
		return int64(number), nil

	case ontologyv1.PropertyTypeDouble:
		number, ok := decoded.(float64)
		if !ok {
			return nil, storedTypeError(property, dataType, decoded)
		}
		return number, nil

	case ontologyv1.PropertyTypeTimestamp:
		text, ok := decoded.(string)
		if !ok {
			return nil, storedTypeError(property, dataType, decoded)
		}
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return nil, fmt.Errorf("property %q: stored value %q is not a timestamp: %w",
				property.Name, text, ErrInvalidValue)
		}
		return parsed, nil

	case ontologyv1.PropertyTypeDate:
		text, ok := decoded.(string)
		if !ok {
			return nil, storedTypeError(property, dataType, decoded)
		}
		parsed, err := time.Parse(DateLayout, text)
		if err != nil {
			return nil, fmt.Errorf("property %q: stored value %q is not a date: %w",
				property.Name, text, ErrInvalidValue)
		}
		return parsed, nil

	case ontologyv1.PropertyTypeArray:
		elements, ok := decoded.([]interface{})
		if !ok {
			return nil, storedTypeError(property, dataType, decoded)
		}
		results := make([]interface{}, 0, len(elements))
		for _, element := range elements {
			result, err := decodeTyped(property, ontologyv1.PropertyType(property.ItemsType), element)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		}
		return results, nil

	case ontologyv1.PropertyTypeJSON:
		return decoded, nil

	default:
		return nil, fmt.Errorf("property %q: unsupported property type %q: %w",
			property.Name, dataType, ErrInvalidValue)
	}
}
