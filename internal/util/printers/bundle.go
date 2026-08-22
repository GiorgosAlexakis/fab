package printers

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
)

func BundleSummary(w io.Writer, bundle *foundryv1.Bundle) error {
	tw := tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)
	fmt.Fprintf(tw, "BUNDLE:\t%s\n", bundle.URL)
	fmt.Fprintf(tw, "REF:\t%s\n", orNone(bundle.Ref))
	fmt.Fprintf(tw, "COMMIT:\t%s\n", ShortRef(bundle.GitRef))
	fmt.Fprintf(tw, "LAYERS:\t%s\n", orNone(strings.Join(bundle.Layers, ", ")))
	return tw.Flush()
}
