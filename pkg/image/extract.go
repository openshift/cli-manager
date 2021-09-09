package image

import (
	"archive/tar"
	"fmt"
	"io"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// Target represents a destination and source target.
type Target struct {
	// Destination is the io.Writer to write to.
	Destination io.Writer

	// Source is the file to extract from the image.
	Source string
}

// ExtractOptions are used for the Extract operation.
type ExtractOptions struct {
	// Tarball, if not-empty, is the io.Writer to write the image's filesystem to as a tarball.
	// If this is set, the Targets option is ignored.
	Tarball io.Writer

	// Targets are a list of source files within the image to copy paired with a destination io.Writer.
	// The same `Target.Source` cannot be specified more than once per extract.
	Targets []Target
}

// Extract an image's filesystem as a tarball, or individual files from the image.
func Extract(img v1.Image, opts *ExtractOptions) error {
	if opts.Tarball != nil {
		return crane.Export(img, opts.Tarball)
	}

	if opts.Targets == nil || len(opts.Targets) == 0 {
		return fmt.Errorf("must provide at least one target")
	}

	targets := map[string]struct{}{}
	for _, t := range opts.Targets {
		if _, ok := targets[t.Source]; ok {
			return fmt.Errorf("duplicate target source detected: %s", t.Source)
		}
		targets[t.Source] = struct{}{}
	}

	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("retrieving image layers: %v", err)
	}

	processedTargets := map[string]struct{}{}

	// we iterate through the layers in reverse order because it makes handling
	// whiteout layers more efficient, since we can just keep track of the removed
	// files as we see .wh. layers and ignore those in previous layers.
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		layerReader, err := layer.Uncompressed()
		if err != nil {
			return fmt.Errorf("reading layer contents: %v", err)
		}
		defer layerReader.Close()

		tarReader := tar.NewReader(layerReader)
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("reading tar: %v", err)
			}

			// skip directories
			if header.Typeflag == tar.TypeDir {
				continue
			}

			// skip empty file contents
			if header.Size == 0 {
				continue
			}

			// some tools prepend everything with "./", so if we don't Clean the
			// name, we may have duplicate entries, which angers tar-split.
			header.Name = filepath.Clean(header.Name)

			// skip empty file names
			if len(header.Name) == 0 {
				continue
			}

			// skip the file if it was already found and processed in a previous/more recent layer
			if _, ok := processedTargets[header.Name]; ok {
				continue
			}

			// determine if we care about the given file
			for _, target := range opts.Targets {
				if header.Name == target.Source {
					processedTargets[header.Name] = struct{}{}
					if _, err := io.Copy(target.Destination, tarReader); err != nil {
						return fmt.Errorf("could not copy %s: %v", header.Name, err)
					}
					break
				}
			}
		}
	}

	return nil
}
