package image

import (
	"io"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// Target represents a destination and source target.
type Target struct {
	// Destination is the io.Writer to write to.
	Destination io.Writer

	// Source is the file to extract from the image.
	// If Source is a directory, the directory will be extracted as a tarball.
	Source string
}

// ExtractOptions are used for the Extract operation.
type ExtractOptions struct {
	// Tarball, if not-empty, is the io.Writer to write the image's filesystem to as a tarball.
	// If this is set, the Targets option is ignored.
	Tarball io.Writer

	// Targets are a pair of source files or directories within the image to copy to the local disk.
	Targets []Target
}

// Extract an image's filesystem as a tarball, or individual files and directories from the image.
func Extract(img v1.Image, opts *ExtractOptions) error {
	if opts.Tarball != nil {
		return crane.Export(img, opts.Tarball)
	}

	// TODO: finish
	return nil
}
