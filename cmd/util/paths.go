package util

import foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"

// The names a foundry is laid out with on disk. The engine owns them because it
// creates the layout; the commands reach for them through here so that a command
// that only needs a file name to put in a message does not import the API package.
const (
	FoundryFileName  = foundryv1.FoundryFileName
	LayersDir        = foundryv1.LayersDir
	ManifestFileName = foundryv1.ManifestFileName
	LockFileName     = foundryv1.LockFileName
	FoundationLayer  = foundryv1.FoundationLayer
)
