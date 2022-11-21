package image

// AuthOptions contains options for handling registries that require authentication.
type AuthOptions struct {
	// Auth is the `.dockercfg` file contents.
	Auth string
}
