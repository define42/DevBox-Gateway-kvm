package gateway

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/define42/gokvm/internal/disk"
	"golang.org/x/sys/unix"
)

// DefaultImageID identifies the operator-configured base image, which cannot be deleted.
const DefaultImageID = "default"

// DefaultMaxImageUploadBytes bounds both uploaded and normalized catalog files.
const DefaultMaxImageUploadBytes int64 = 20 << 30

const maxImageVirtualSize int64 = 1 << 40

var (
	ErrImageTooLarge        = errors.New("image exceeds the upload or normalized size limit")
	ErrImageToolUnavailable = errors.New("install qemu-utils to import compressed or unsupported QCOW2 layouts")
)

// Image describes an immutable desktop template. Host paths are never exposed.
type Image struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	SizeBytes        int64     `json:"sizeBytes"`
	VirtualSizeBytes int64     `json:"virtualSizeBytes"`
	CreatedAt        time.Time `json:"createdAt"`
	Default          bool      `json:"default"`
}

// ImageBackend supports a shared image catalog and explicit template selection.
// Its caller must authorize image imports and deletion as administrator actions.
type ImageBackend interface {
	Images(context.Context) ([]Image, error)
	ImportImage(ctx context.Context, name string, input io.Reader) (Image, error)
	DeleteImage(ctx context.Context, id string) error
	CreateWithImage(ctx context.Context, owner, name, passwordHash, imageID string) (VM, error)
}

// AdminBackend exposes inventory for an already authorized administrator.
// Mutations still use the resolved owner's identity with the normal Backend methods.
type AdminBackend interface {
	ListAll(context.Context) ([]VM, error)
	GetAny(ctx context.Context, id string) (VM, error)
}

func validImageName(name string) bool {
	if name == "" || len(name) > 128 || strings.TrimSpace(name) != name || name == "." || name == ".." {
		return false
	}
	for _, char := range name {
		if char < 32 || char == 127 || char == '/' || char == '\\' {
			return false
		}
	}

	return true
}

func (m *Manager) imageDirectory(id string) string {
	return filepath.Join(m.config.DataDir, "images", id)
}

func (m *Manager) loadImages() error {
	root := filepath.Join(m.config.DataDir, "images")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if err := checkPrivateDirectory(root); err != nil {
		return err
	}
	base, err := disk.OpenReadOnly(m.config.BaseImage)
	if err != nil {
		return err
	}
	virtualSize := base.Size()
	if err := base.Close(); err != nil {
		return err
	}
	info, err := os.Stat(m.config.BaseImage)
	if err != nil {
		return err
	}
	m.images[DefaultImageID] = Image{
		ID: DefaultImageID, Name: filepath.Base(m.config.BaseImage),
		SizeBytes: info.Size(), VirtualSizeBytes: virtualSize, CreatedAt: info.ModTime().UTC(), Default: true,
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if id, staging := strings.CutPrefix(entry.Name(), ".importing-"); staging && validID(id) {
			// The exclusive manager lock means this is an interrupted upload.
			if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return err
			}

			continue
		}
		if !entry.IsDir() || !validID(entry.Name()) {
			return fmt.Errorf("unexpected image catalog entry: %w", ErrInvalid)
		}
		if err := m.loadImage(entry.Name()); err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) loadImage(id string) error {
	dir := m.imageDirectory(id)
	if err := checkPrivateDirectory(dir); err != nil {
		return err
	}
	data, err := readPrivateFile(filepath.Join(dir, "image.json"), 4096)
	if err != nil {
		return fmt.Errorf("read image metadata: %w", err)
	}
	var image Image
	if err := json.Unmarshal(data, &image); err != nil {
		return fmt.Errorf("decode image metadata: %w", err)
	}
	if image.ID != id || !validImageName(image.Name) || image.Default || image.CreatedAt.IsZero() ||
		image.SizeBytes <= 0 || image.VirtualSizeBytes <= 0 || image.VirtualSizeBytes > maxImageVirtualSize {
		return fmt.Errorf("invalid saved image metadata: %w", ErrInvalid)
	}
	for _, existing := range m.images {
		if strings.EqualFold(image.Name, existing.Name) {
			return fmt.Errorf("duplicate saved image name: %w", ErrInvalid)
		}
	}
	path := filepath.Join(dir, "disk.qcow2")
	file, err := openPrivateFile(path, os.O_RDONLY)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	if info.Size() != image.SizeBytes {
		return fmt.Errorf("saved image size changed: %w", ErrInvalid)
	}
	if _, err := validateUploadedQCOW2(path); err != nil {
		return err
	}
	opened, err := disk.OpenReadOnly(path)
	if err != nil {
		return fmt.Errorf("saved image is unsupported: %w", err)
	}
	virtualSize := opened.Size()
	if err := opened.Close(); err != nil {
		return err
	}
	if virtualSize != image.VirtualSizeBytes {
		return fmt.Errorf("saved image virtual size changed: %w", ErrInvalid)
	}
	m.images[id] = image

	return nil
}

// Images returns a copy of the shared catalog, with the default template first.
func (m *Manager) Images(ctx context.Context) ([]Image, error) {
	_, finish, err := m.operation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	m.imageMu.RLock()
	defer m.imageMu.RUnlock()
	images := make([]Image, 0, len(m.images))
	for _, image := range m.images {
		images = append(images, image)
	}
	slices.SortFunc(images, func(a, b Image) int {
		if a.Default != b.Default {
			if a.Default {
				return -1
			}

			return 1
		}

		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

	return images, nil
}

// ImportImage streams a standalone QCOW2 template into a private staging directory.
// Compressed or unsupported layouts are normalized before publication. Only one
// import runs at a time, and interrupted imports never become selectable templates.
func (m *Manager) ImportImage(ctx context.Context, name string, input io.Reader) (Image, error) {
	ctx, finish, err := m.operation(ctx)
	if err != nil {
		return Image{}, err
	}
	defer finish()
	if !validImageName(name) || input == nil {
		return Image{}, fmt.Errorf("invalid image name or empty upload: %w", ErrInvalid)
	}
	select {
	case m.imageImports <- struct{}{}:
		defer func() { <-m.imageImports }()
	case <-ctx.Done():
		return Image{}, ctx.Err()
	}
	m.imageMu.RLock()
	for _, existing := range m.images {
		if strings.EqualFold(existing.Name, name) {
			m.imageMu.RUnlock()

			return Image{}, fmt.Errorf("an image with this name already exists: %w", ErrConflict)
		}
	}
	m.imageMu.RUnlock()
	image := Image{ID: strings.ToLower(rand.Text()), Name: name, CreatedAt: time.Now().UTC()}
	root := filepath.Join(m.config.DataDir, "images")
	staging := filepath.Join(root, ".importing-"+image.ID)
	if err := os.Mkdir(staging, 0o700); err != nil {
		return Image{}, err
	}
	defer func() {
		_ = os.RemoveAll(staging) //nolint:gosec // Only the server-generated private import directory.
	}()
	upload := filepath.Join(staging, "upload.qcow2")
	if err := saveImageUpload(ctx, upload, input, m.config.MaxImageUploadBytes); err != nil {
		return Image{}, err
	}
	image.VirtualSizeBytes, err = validateUploadedQCOW2(upload)
	if err != nil {
		return Image{}, err
	}
	prepared := filepath.Join(staging, "disk.qcow2")
	if err := normalizeUploadedQCOW2(ctx, upload, prepared, m.config.MaxImageUploadBytes); err != nil {
		return Image{}, err
	}
	info, err := os.Stat(prepared)
	if err != nil {
		return Image{}, err
	}
	image.SizeBytes = info.Size()
	if err := m.publishImage(ctx, image, staging); err != nil {
		return Image{}, err
	}

	return image, nil
}

func saveImageUpload(ctx context.Context, path string, input io.Reader, limit int64) (saveErr error) {
	file, err := openPrivateFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer func() { saveErr = errors.Join(saveErr, file.Close()) }()
	limited := io.LimitReader(imageContextReader{ctx: ctx, reader: input}, limit+1)
	written, err := io.CopyBuffer(file, limited, make([]byte, 1<<20))
	if err != nil {
		return fmt.Errorf("read image upload: %w", err)
	}
	if written > limit {
		return ErrImageTooLarge
	}
	if written == 0 {
		return fmt.Errorf("image upload is empty: %w", ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return file.Sync()
}

type imageContextReader struct {
	ctx    context.Context //nolint:containedctx // Carries cancellation through one streaming operation.
	reader io.Reader
}

func (r imageContextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	return r.reader.Read(buffer)
}

func (m *Manager) publishImage(ctx context.Context, image Image, staging string) error {
	data, err := json.MarshalIndent(image, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(staging, "image.json", data); err != nil {
		return err
	}
	m.imageMu.Lock()
	defer m.imageMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	destination := m.imageDirectory(image.ID)
	if err := unix.Renameat2(unix.AT_FDCWD, staging, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err := syncImageDirectory(filepath.Dir(destination)); err != nil {
		_ = os.RemoveAll(destination) //nolint:gosec // Only the newly published server-generated image directory.

		return err
	}
	m.images[image.ID] = image

	return nil
}

func syncImageDirectory(path string) error {
	file, err := os.Open(path) //nolint:gosec // Fixed catalog directory below the private state root.
	if err != nil {
		return err
	}
	defer file.Close()

	return file.Sync()
}

// DeleteImage removes an uploaded template. Existing VMs retain their independent
// copies. Holding the catalog write lock lets any in-flight VM clone finish first.
func (m *Manager) DeleteImage(ctx context.Context, id string) error {
	ctx, finish, err := m.operation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if id == DefaultImageID {
		return fmt.Errorf("the configured default image cannot be removed: %w", ErrConflict)
	}
	if !validID(id) {
		return ErrNotFound
	}
	m.imageMu.Lock()
	defer m.imageMu.Unlock()
	if _, exists := m.images[id]; !exists {
		return ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := m.imageDirectory(id)
	if err := checkPrivateDirectory(dir); err != nil {
		return err
	}
	// Rename before cleanup so a crash cannot leave a partially deleted catalog entry.
	staging := filepath.Join(filepath.Dir(dir), ".importing-"+id)
	if err := unix.Renameat2(unix.AT_FDCWD, dir, unix.AT_FDCWD, staging, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	delete(m.images, id)
	removeErr := os.RemoveAll(staging) //nolint:gosec // Validated generated ID in the private catalog.

	return errors.Join(removeErr, syncImageDirectory(filepath.Dir(dir)))
}

// selectedImage requires imageMu to remain read-locked until the clone completes.
func (m *Manager) selectedImage(id string) (Image, string, error) {
	if id == "" {
		id = DefaultImageID
	}
	image, exists := m.images[id]
	if !exists {
		return Image{}, "", fmt.Errorf("image not found: %w", ErrNotFound)
	}
	if image.Default {
		return image, m.config.BaseImage, nil
	}
	path := filepath.Join(m.imageDirectory(id), "disk.qcow2")
	if err := checkPrivateDirectory(m.imageDirectory(id)); err != nil {
		return Image{}, "", err
	}
	file, err := openPrivateFile(path, os.O_RDONLY)
	if err != nil {
		return Image{}, "", err
	}
	if err := file.Close(); err != nil {
		return Image{}, "", err
	}

	return image, path, nil
}

// ListAll returns every owner's VMs; callers must already be authorized admins.
func (m *Manager) ListAll(ctx context.Context) ([]VM, error) {
	ctx, finish, err := m.operation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	m.mu.Lock()
	list := make([]*managedVM, 0, len(m.vms))
	for _, vm := range m.vms {
		list = append(list, vm)
	}
	m.mu.Unlock()
	result := make([]VM, 0, len(list))
	for _, vm := range list {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		vm.mu.Lock()
		result = append(result, vm.record)
		vm.mu.Unlock()
	}
	slices.SortFunc(result, func(a, b VM) int { return a.CreatedAt.Compare(b.CreatedAt) })

	return result, nil
}

// GetAny resolves the actual owner of an administrator-selected VM.
func (m *Manager) GetAny(ctx context.Context, id string) (VM, error) {
	_, finish, err := m.operation(ctx)
	if err != nil {
		return VM{}, err
	}
	defer finish()
	m.mu.Lock()
	vm := m.vms[id]
	m.mu.Unlock()
	if vm == nil {
		return VM{}, ErrNotFound
	}
	vm.mu.Lock()
	defer vm.mu.Unlock()

	return vm.record, nil
}
