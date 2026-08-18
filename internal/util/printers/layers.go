package printers

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	layerv1 "github.com/GiorgosAlexakis/fab/internal/layer/v1"
)

func LayerList(w io.Writer, discovered []*layerv1.Layer) error {
	tw := tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)
	fmt.Fprintln(tw, "LAYER\tVERSION\tORIGIN\tDEPENDS ON")
	for _, layer := range discovered {
		dependencies := make([]string, 0, len(layer.Spec.DependsOn))
		for _, dependency := range layer.Spec.DependsOn {
			dependencies = append(dependencies, dependency.Name)
		}

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			layer.Metadata.Name,
			layer.Metadata.Version,
			layer.Metadata.Origin,
			orNone(strings.Join(dependencies, ",")),
		)
	}
	return tw.Flush()
}
