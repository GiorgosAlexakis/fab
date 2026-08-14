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
	"errors"
	"reflect"
	"testing"
	"time"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
)

func property(name string, dataType ontologyv1.PropertyType) Property {
	return Property{ID: 1, Name: name, DataType: string(dataType), Nullable: true}
}

func TestEncodeValue(t *testing.T) {
	tests := []struct {
		name     string
		property Property
		value    interface{}
		want     string
	}{{
		name:     "string",
		property: property("name", ontologyv1.PropertyTypeString),
		value:    "Ada",
		want:     `"Ada"`,
	}, {
		name:     "boolean",
		property: property("active", ontologyv1.PropertyTypeBoolean),
		value:    true,
		want:     `true`,
	}, {
		name:     "integer from a JSON-decoded float",
		property: property("count", ontologyv1.PropertyTypeInteger),
		value:    float64(42),
		want:     `42`,
	}, {
		name:     "long",
		property: property("bytes", ontologyv1.PropertyTypeLong),
		value:    int64(1 << 40),
		want:     `1099511627776`,
	}, {
		name:     "double",
		property: property("ratio", ontologyv1.PropertyTypeDouble),
		value:    0.5,
		want:     `0.5`,
	}, {
		// A decimal is stored as text precisely so that it never passes through
		// binary floating point.
		name:     "decimal keeps its trailing zero",
		property: property("total", ontologyv1.PropertyTypeDecimal),
		value:    "10.50",
		want:     `"10.50"`,
	}, {
		name:     "timestamp normalises to UTC",
		property: property("placed_at", ontologyv1.PropertyTypeTimestamp),
		value:    "2024-03-01T12:00:00+02:00",
		want:     `"2024-03-01T10:00:00Z"`,
	}, {
		name:     "date",
		property: property("born_on", ontologyv1.PropertyTypeDate),
		value:    time.Date(1815, 12, 10, 3, 4, 5, 0, time.UTC),
		want:     `"1815-12-10"`,
	}, {
		name: "enum",
		property: Property{ID: 1, Name: "tier", DataType: string(ontologyv1.PropertyTypeEnum),
			EnumValues: []string{"free", "pro"}},
		value: "pro",
		want:  `"pro"`,
	}, {
		name: "array of strings",
		property: Property{ID: 1, Name: "tags", DataType: string(ontologyv1.PropertyTypeArray),
			ItemsType: string(ontologyv1.PropertyTypeString)},
		value: []string{"vip", "eu"},
		want:  `["vip","eu"]`,
	}, {
		name:     "json document",
		property: property("preferences", ontologyv1.PropertyTypeJSON),
		value:    map[string]interface{}{"theme": "dark"},
		want:     `{"theme":"dark"}`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := EncodeValue(test.property, test.value)
			if err != nil {
				t.Fatalf("EncodeValue() failed: %v", err)
			}
			if string(encoded) != test.want {
				t.Errorf("EncodeValue() = %s, want %s", encoded, test.want)
			}
		})
	}
}

func TestEncodeValueRejects(t *testing.T) {
	tests := []struct {
		name     string
		property Property
		value    interface{}
	}{{
		name:     "nil",
		property: property("name", ontologyv1.PropertyTypeString),
		value:    nil,
	}, {
		name:     "a number where a string is declared",
		property: property("name", ontologyv1.PropertyTypeString),
		value:    7,
	}, {
		name:     "a fractional value for an integer",
		property: property("count", ontologyv1.PropertyTypeInteger),
		value:    1.5,
	}, {
		name:     "an integer too large for its declared width",
		property: property("count", ontologyv1.PropertyTypeInteger),
		value:    int64(1) << 40,
	}, {
		// Money must not arrive as a float: the caller has already lost
		// precision by the time it gets here.
		name:     "a float for a decimal",
		property: property("total", ontologyv1.PropertyTypeDecimal),
		value:    10.5,
	}, {
		name:     "a malformed decimal",
		property: property("total", ontologyv1.PropertyTypeDecimal),
		value:    "10.50 EUR",
	}, {
		name:     "a timestamp without a zone",
		property: property("placed_at", ontologyv1.PropertyTypeTimestamp),
		value:    "2024-03-01 12:00:00",
	}, {
		name: "a value outside an enum",
		property: Property{Name: "tier", DataType: string(ontologyv1.PropertyTypeEnum),
			EnumValues: []string{"free", "pro"}},
		value: "platinum",
	}, {
		name: "a null inside an array",
		property: Property{Name: "tags", DataType: string(ontologyv1.PropertyTypeArray),
			ItemsType: string(ontologyv1.PropertyTypeString)},
		value: []interface{}{"vip", nil},
	}, {
		name: "an element of the wrong type",
		property: Property{Name: "tags", DataType: string(ontologyv1.PropertyTypeArray),
			ItemsType: string(ontologyv1.PropertyTypeString)},
		value: []interface{}{"vip", 3},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := EncodeValue(test.property, test.value); !errors.Is(err, ErrInvalidValue) {
				t.Errorf("EncodeValue() error = %v, want ErrInvalidValue", err)
			}
		})
	}
}

func TestValueRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		property Property
		value    interface{}
		want     interface{}
	}{{
		name:     "string",
		property: property("name", ontologyv1.PropertyTypeString),
		value:    "Ada",
		want:     "Ada",
	}, {
		name:     "long",
		property: property("bytes", ontologyv1.PropertyTypeLong),
		value:    int64(1 << 40),
		want:     int64(1 << 40),
	}, {
		name:     "double",
		property: property("ratio", ontologyv1.PropertyTypeDouble),
		value:    0.5,
		want:     0.5,
	}, {
		name:     "decimal stays text",
		property: property("total", ontologyv1.PropertyTypeDecimal),
		value:    "10.50",
		want:     "10.50",
	}, {
		name:     "timestamp",
		property: property("placed_at", ontologyv1.PropertyTypeTimestamp),
		value:    time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC),
		want:     time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC),
	}, {
		name: "array of longs",
		property: Property{Name: "sizes", DataType: string(ontologyv1.PropertyTypeArray),
			ItemsType: string(ontologyv1.PropertyTypeLong)},
		value: []int64{1, 2, 3},
		want:  []interface{}{int64(1), int64(2), int64(3)},
	}, {
		name:     "json document",
		property: property("preferences", ontologyv1.PropertyTypeJSON),
		value:    map[string]interface{}{"theme": "dark", "density": 2.0},
		want:     map[string]interface{}{"theme": "dark", "density": 2.0},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := EncodeValue(test.property, test.value)
			if err != nil {
				t.Fatalf("EncodeValue() failed: %v", err)
			}
			decoded, err := DecodeValue(test.property, encoded)
			if err != nil {
				t.Fatalf("DecodeValue() failed: %v", err)
			}
			if !reflect.DeepEqual(decoded, test.want) {
				t.Errorf("round trip = %#v, want %#v", decoded, test.want)
			}
		})
	}
}

func TestDecodeValueOfAnAbsentRow(t *testing.T) {
	decoded, err := DecodeValue(property("name", ontologyv1.PropertyTypeString), nil)
	if err != nil {
		t.Fatalf("DecodeValue() failed: %v", err)
	}
	if decoded != nil {
		t.Errorf("DecodeValue(nil) = %#v, want nil", decoded)
	}
}

// TestUniqueKeyCanonicalisesNumbers is what keeps object_prop_unique honest: two
// values that are equal must claim the same key, whatever Go type they arrived
// as.
func TestUniqueKeyCanonicalisesNumbers(t *testing.T) {
	long := property("serial", ontologyv1.PropertyTypeLong)

	fromInt, err := UniqueKey(long, 1)
	if err != nil {
		t.Fatalf("UniqueKey(int) failed: %v", err)
	}
	fromFloat, err := UniqueKey(long, float64(1))
	if err != nil {
		t.Fatalf("UniqueKey(float64) failed: %v", err)
	}
	if fromInt != fromFloat {
		t.Errorf("1 and 1.0 produced different unique keys: %q and %q", fromInt, fromFloat)
	}
}

func TestUniqueKeyRejectsNonScalars(t *testing.T) {
	document := property("preferences", ontologyv1.PropertyTypeJSON)
	if _, err := UniqueKey(document, map[string]interface{}{"a": 1}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("UniqueKey() error = %v, want ErrInvalidValue", err)
	}
}

func TestParseValue(t *testing.T) {
	tests := []struct {
		name     string
		property Property
		text     string
		want     interface{}
	}{{
		name:     "string",
		property: property("name", ontologyv1.PropertyTypeString),
		text:     "Ada",
		want:     "Ada",
	}, {
		name:     "boolean",
		property: property("active", ontologyv1.PropertyTypeBoolean),
		text:     "true",
		want:     true,
	}, {
		name:     "long",
		property: property("bytes", ontologyv1.PropertyTypeLong),
		text:     "1099511627776",
		want:     int64(1099511627776),
	}, {
		name:     "double",
		property: property("ratio", ontologyv1.PropertyTypeDouble),
		text:     "0.5",
		want:     0.5,
	}, {
		name:     "decimal stays text so that precision survives",
		property: property("total", ontologyv1.PropertyTypeDecimal),
		text:     "10.50",
		want:     "10.50",
	}, {
		name: "comma-separated array",
		property: Property{Name: "tags", DataType: string(ontologyv1.PropertyTypeArray),
			ItemsType: string(ontologyv1.PropertyTypeString)},
		text: "vip, eu",
		want: []interface{}{"vip", "eu"},
	}, {
		name: "JSON array",
		property: Property{Name: "sizes", DataType: string(ontologyv1.PropertyTypeArray),
			ItemsType: string(ontologyv1.PropertyTypeLong)},
		text: "[1, 2]",
		want: []interface{}{float64(1), float64(2)},
	}, {
		name:     "json document",
		property: property("preferences", ontologyv1.PropertyTypeJSON),
		text:     `{"theme":"dark"}`,
		want:     map[string]interface{}{"theme": "dark"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := ParseValue(test.property, test.text)
			if err != nil {
				t.Fatalf("ParseValue() failed: %v", err)
			}
			if !reflect.DeepEqual(parsed, test.want) {
				t.Errorf("ParseValue() = %#v, want %#v", parsed, test.want)
			}
		})
	}
}

func TestParseValueRejectsMalformedText(t *testing.T) {
	tests := []struct {
		name     string
		property Property
		text     string
	}{
		{"boolean", property("active", ontologyv1.PropertyTypeBoolean), "yes please"},
		{"long", property("bytes", ontologyv1.PropertyTypeLong), "12kb"},
		{"double", property("ratio", ontologyv1.PropertyTypeDouble), "half"},
		{"json", property("preferences", ontologyv1.PropertyTypeJSON), "{theme}"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseValue(test.property, test.text); !errors.Is(err, ErrInvalidValue) {
				t.Errorf("ParseValue() error = %v, want ErrInvalidValue", err)
			}
		})
	}
}
