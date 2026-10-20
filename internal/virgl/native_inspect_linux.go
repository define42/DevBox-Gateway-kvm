//go:build linux && cgo && virgl && integration

package virgl

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>

// Use virglrenderer's already-loaded GL dispatcher without adding a test-only
// dependency on OpenGL development headers. This reads state; it never changes
// the current context or its bindings.
static int devbox_test_texture_binding(int *binding) {
    typedef void (*get_integer_fn)(unsigned int, int *);
    get_integer_fn *get_integer = dlsym(RTLD_DEFAULT, "epoxy_glGetIntegerv");
    if (!get_integer) return -1;
    (*get_integer)(0x8069, binding); // GL_TEXTURE_BINDING_2D
    return 0;
}
*/
import "C"

// nativeTextureBinding must run on the renderer's locked OS thread. The helper
// lives outside _test.go because Go does not support cgo imports in test files.
func nativeTextureBinding() (int, error) {
	var binding C.int
	err := nativeError("query texture binding", C.devbox_test_texture_binding(&binding))

	return int(binding), err
}
