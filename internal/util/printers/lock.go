package printers

import (
	"fmt"
	"io"
	"text/tabwriter"

	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
)

func LockSummary(w io.Writer, lock *foundryv1.Lock) error {
	tw := tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)
	fmt.Fprintln(tw, "LAYER\tVERSION\tORIGIN\tDIGEST")
	for _, entry := range lock.Locked {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			entry.Name, entry.Version, entry.Origin, ShortDigest(entry.Digest))
	}
	return tw.Flush()
}
