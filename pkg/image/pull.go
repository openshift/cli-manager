package image

import (
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// PullOptions are used for the Pull operation.
type PullOptions struct {
	AuthOptions
}

// Pull an image down to the local filesystem.
func Pull(src string, opts *PullOptions) (v1.Image, error) {
	if opts == nil {
		opts = &PullOptions{}
	}

	craneOptions := []crane.Option{}
	if len(opts.Auth) > 0 {
		auth := authn.FromConfig(authn.AuthConfig{
			Auth: opts.Auth,
		})
		craneOptions = append(craneOptions, crane.WithAuth(auth))
	}

	return crane.Pull(src, craneOptions...)
}
