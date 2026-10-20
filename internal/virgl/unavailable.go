//go:build !linux || !cgo || !virgl

package virgl

// Available reports whether native virgl support was compiled into this binary.
func Available() bool { return false }

// New reports that this binary was built without the optional native backend.
func New() (*Renderer, error) { return nil, ErrUnavailable }
