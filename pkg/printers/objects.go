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

package printers

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"k8s.io/apimachinery/pkg/util/duration"

	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
)

// MaxCellWidth bounds a property value in table output. Long values are
// truncated rather than wrapped, because a wrapped row is unreadable and the
// full value is one -o json away.
const MaxCellWidth = 32

// Object writes one object instance and its current property values.
func Object(w io.Writer, object *objectstore.Object) error {
	tw := tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)
	fmt.Fprintf(tw, "TYPE:\t%s\n", object.Type)
	fmt.Fprintf(tw, "PRIMARY KEY:\t%s\n", object.PrimaryKey)
	fmt.Fprintf(tw, "CREATED:\t%s\n", object.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(tw, "UPDATED:\t%s\n", object.UpdatedAt.Format(time.RFC3339))
	if err := tw.Flush(); err != nil {
		return err
	}

	if len(object.Properties) == 0 {
		return nil
	}

	fmt.Fprintln(w, "\nPROPERTIES:")
	properties := tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)
	for _, name := range sortedKeys(object.Properties) {
		fmt.Fprintf(properties, "  %s\t%s\n", name, FormatValue(object.Properties[name]))
	}
	return properties.Flush()
}

// ObjectList writes a table of objects with one column per named property.
//
// The columns come from the ontology rather than from the objects, so that a
// property left unset on some rows still gets a column and the table has the
// same shape for every page.
func ObjectList(w io.Writer, objects []objectstore.Object, properties []string) error {
	tw := tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)

	header := make([]string, 0, len(properties)+2)
	header = append(header, "PRIMARY KEY")
	for _, property := range properties {
		header = append(header, strings.ToUpper(property))
	}
	header = append(header, "AGE")
	fmt.Fprintln(tw, strings.Join(header, "\t"))

	for i := range objects {
		object := &objects[i]
		cells := make([]string, 0, len(properties)+2)
		cells = append(cells, object.PrimaryKey)
		for _, property := range properties {
			value, ok := object.Properties[property]
			if !ok {
				cells = append(cells, "<unset>")
				continue
			}
			cells = append(cells, truncate(FormatValue(value)))
		}
		cells = append(cells, duration.HumanDuration(time.Since(object.CreatedAt)))
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}

	return tw.Flush()
}

// FormatValue renders a property value for human-readable output.
func FormatValue(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return "<unset>"
	case string:
		return typed
	case time.Time:
		return typed.Format(time.RFC3339)
	case []interface{}, map[string]interface{}:
		// Arrays and JSON documents have no better one-line form than JSON.
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(encoded)
	default:
		return fmt.Sprint(typed)
	}
}

func truncate(value string) string {
	if len(value) <= MaxCellWidth {
		return value
	}
	return value[:MaxCellWidth-3] + "..."
}

func sortedKeys(values map[string]interface{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
