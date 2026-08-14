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
	"regexp"
	"strconv"
	"time"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
)

// decimalPattern matches the canonical text form of a decimal.
var decimalPattern = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

func asString(property Property, value interface{}) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", typeError(property, ontologyv1.PropertyType(property.DataType), value)
	}
	return text, nil
}

func asInt64(property Property, dataType ontologyv1.PropertyType, value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int64:
		return typed, nil
	case json.Number:
		integer, err := typed.Int64()
		if err != nil {
			return 0, typeError(property, dataType, value)
		}
		return integer, nil
	case float64:
		// JSON has one number type, so an integral float is how an integer
		// arrives from a decoded request body.
		if typed != math.Trunc(typed) {
			return 0, fmt.Errorf("property %q: %v has a fractional part but the property is %s: %w",
				property.Name, typed, dataType, ErrInvalidValue)
		}
		return int64(typed), nil
	default:
		return 0, typeError(property, dataType, value)
	}
}

func asFloat64(property Property, dataType ontologyv1.PropertyType, value interface{}) (float64, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	case int32:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case json.Number:
		number, err := typed.Float64()
		if err != nil {
			return 0, typeError(property, dataType, value)
		}
		return number, nil
	default:
		return 0, typeError(property, dataType, value)
	}
}

// asDecimal stores decimals as text. A decimal exists precisely because binary
// floating point is the wrong representation for money, so it must not pass
// through float64 on its way into storage.
func asDecimal(property Property, value interface{}) (string, error) {
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case json.Number:
		text = typed.String()
	case int:
		text = strconv.Itoa(typed)
	case int64:
		text = strconv.FormatInt(typed, 10)
	default:
		return "", fmt.Errorf("property %q: a decimal must be given as a string or an integer, not %T, "+
			"so that precision is not lost: %w", property.Name, value, ErrInvalidValue)
	}

	if !decimalPattern.MatchString(text) {
		return "", fmt.Errorf("property %q: %q is not a decimal: %w", property.Name, text, ErrInvalidValue)
	}
	return text, nil
}

func asTimestamp(property Property, value interface{}) (string, error) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano), nil
	case string:
		parsed, err := time.Parse(time.RFC3339, typed)
		if err != nil {
			return "", fmt.Errorf("property %q: %q is not an RFC 3339 timestamp: %w",
				property.Name, typed, ErrInvalidValue)
		}
		return parsed.UTC().Format(time.RFC3339Nano), nil
	default:
		return "", typeError(property, ontologyv1.PropertyTypeTimestamp, value)
	}
}

func asDate(property Property, value interface{}) (string, error) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC().Format(DateLayout), nil
	case string:
		parsed, err := time.Parse(DateLayout, typed)
		if err != nil {
			return "", fmt.Errorf("property %q: %q is not a date of the form %s: %w",
				property.Name, typed, DateLayout, ErrInvalidValue)
		}
		return parsed.Format(DateLayout), nil
	default:
		return "", typeError(property, ontologyv1.PropertyTypeDate, value)
	}
}

func asArray(property Property, value interface{}) (interface{}, error) {
	elements, err := toSlice(value)
	if err != nil {
		return nil, typeError(property, ontologyv1.PropertyTypeArray, value)
	}

	itemsType := ontologyv1.PropertyType(property.ItemsType)
	results := make([]interface{}, 0, len(elements))
	for i, element := range elements {
		if element == nil {
			return nil, fmt.Errorf("property %q: element %d is null; arrays hold no nulls: %w",
				property.Name, i, ErrInvalidValue)
		}
		normalized, err := normalizeValue(property, itemsType, element)
		if err != nil {
			return nil, err
		}
		results = append(results, normalized)
	}
	return results, nil
}

func toSlice(value interface{}) ([]interface{}, error) {
	switch typed := value.(type) {
	case []interface{}:
		return typed, nil
	case []string:
		elements := make([]interface{}, len(typed))
		for i := range typed {
			elements[i] = typed[i]
		}
		return elements, nil
	case []int:
		elements := make([]interface{}, len(typed))
		for i := range typed {
			elements[i] = typed[i]
		}
		return elements, nil
	case []int64:
		elements := make([]interface{}, len(typed))
		for i := range typed {
			elements[i] = typed[i]
		}
		return elements, nil
	case []float64:
		elements := make([]interface{}, len(typed))
		for i := range typed {
			elements[i] = typed[i]
		}
		return elements, nil
	default:
		return nil, fmt.Errorf("not a slice")
	}
}

func typeError(property Property, dataType ontologyv1.PropertyType, value interface{}) error {
	return fmt.Errorf("property %q: %T is not a valid %s value: %w",
		property.Name, value, dataType, ErrInvalidValue)
}

func storedTypeError(property Property, dataType ontologyv1.PropertyType, value interface{}) error {
	return fmt.Errorf("property %q: stored value %v does not match the declared type %s: %w",
		property.Name, value, dataType, ErrInvalidValue)
}
