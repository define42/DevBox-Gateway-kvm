// Package workersandbox confines managed VM processes without host capabilities.
// Filesystem and IPC restrictions are installed before re-exec so every Go and
// native thread inherits them. A second syscall filter is installed before any
// guest code or remote RDP input is handled.
package workersandbox
