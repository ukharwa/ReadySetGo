package uploads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
)

// Storage keeps uploaded files' contents under keys the Service chooses.
// It is the seam for moving files to object storage (S3) when the app runs
// on more than one server; SCALING.md describes how.
type Storage interface {
	// Put stores r's contents under key and returns how many bytes it
	// wrote. If it fails, nothing is left under key.
	Put(ctx context.Context, key string, r io.Reader) (int64, error)
	// Open returns the contents stored under key.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes key's contents. Deleting a missing key is not an
	// error.
	Delete(ctx context.Context, key string) error
}

// validKey matches the keys the Service makes (rand.Text), so a key can
// never name a path outside the folder.
var validKey = regexp.MustCompile(`^[A-Z2-7]{26}$`)

// Disk is Storage in a folder on the local disk. Every instance of the app
// must see the same folder, so beyond one server, use object storage.
type Disk struct {
	root *os.Root
}

// NewDisk returns Disk storage in dir, creating the folder if needed.
func NewDisk(dir string) (*Disk, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create uploads folder: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open uploads folder: %w", err)
	}
	return &Disk{root: root}, nil
}

// Close releases the folder.
func (d *Disk) Close() error { return d.root.Close() }

func checkKey(key string) error {
	if !validKey.MatchString(key) {
		return fmt.Errorf("invalid storage key %q", key)
	}
	return nil
}

// Put writes to a temporary file and renames it into place once complete,
// so a reader never sees half a file.
func (d *Disk) Put(_ context.Context, key string, r io.Reader) (int64, error) {
	if err := checkKey(key); err != nil {
		return 0, err
	}
	tmp := key + ".tmp"
	f, err := d.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, r)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = d.root.Rename(tmp, key)
	}
	if err != nil {
		_ = d.root.Remove(tmp)
		return n, err
	}
	return n, nil
}

// Open opens key's file.
func (d *Disk) Open(_ context.Context, key string) (io.ReadCloser, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	return d.root.Open(key)
}

// Delete removes key's file.
func (d *Disk) Delete(_ context.Context, key string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if err := d.root.Remove(key); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
