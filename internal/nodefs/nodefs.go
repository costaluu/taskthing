// Package nodefs gives filesystem failures the shape Bun/Node gives them —
// `ENOENT: no such file or directory, open '<path>'` — since those messages
// reach the user inside taskthing's failure lines, and reads directories in
// the order readdir returns them (unsorted).
package nodefs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

var descriptions = map[syscall.Errno][2]string{
	syscall.ENOENT:    {"ENOENT", "no such file or directory"},
	syscall.EEXIST:    {"EEXIST", "file already exists"},
	syscall.ENOTDIR:   {"ENOTDIR", "not a directory"},
	syscall.EISDIR:    {"EISDIR", "illegal operation on a directory"},
	syscall.EACCES:    {"EACCES", "permission denied"},
	syscall.EPERM:     {"EPERM", "operation not permitted"},
	syscall.ENOTEMPTY: {"ENOTEMPTY", "directory not empty"},
	syscall.EXDEV:     {"EXDEV", "cross-device link not permitted"},
	syscall.EBUSY:     {"EBUSY", "resource busy or locked"},
	syscall.EINVAL:    {"EINVAL", "invalid argument"},
	syscall.ENOSPC:    {"ENOSPC", "no space left on device"},
	syscall.EROFS:     {"EROFS", "read-only file system"},
}

func describe(err error) (string, bool) {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if d, ok := descriptions[errno]; ok {
			return d[0] + ": " + d[1], true
		}
		return errno.Error(), true
	}
	return "", false
}

// Error is a filesystem failure carrying Node's message.
type Error struct {
	msg string
	err error
}

func (e *Error) Error() string { return e.msg }
func (e *Error) Unwrap() error { return e.err }

// Wrap renders err as `<CODE>: <description>, <syscall> '<path>'`.
func Wrap(syscallName, path string, err error) error {
	if err == nil {
		return nil
	}
	if d, ok := describe(err); ok {
		return &Error{msg: fmt.Sprintf("%s, %s '%s'", d, syscallName, path), err: err}
	}
	return err
}

// WrapRename renders a rename failure the way Node does: `… rename 'a' -> 'b'`.
func WrapRename(from, to string, err error) error {
	if err == nil {
		return nil
	}
	if d, ok := describe(err); ok {
		return &Error{msg: fmt.Sprintf("%s, rename '%s' -> '%s'", d, from, to), err: err}
	}
	return err
}

// Readdir lists a directory's names in the order the OS returns them.
func Readdir(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, Wrap("scandir", dir, err)
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil, Wrap("scandir", dir, err)
	}
	return names, nil
}

// Entry is a readdir entry with its type.
type Entry struct {
	Name  string
	IsDir bool
}

// ReaddirTyped is readdir with { withFileTypes: true }.
func ReaddirTyped(dir string) ([]Entry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, Wrap("scandir", dir, err)
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, Wrap("scandir", dir, err)
	}
	out := make([]Entry, len(entries))
	for i, e := range entries {
		out[i] = Entry{Name: e.Name(), IsDir: e.Type()&fs.ModeDir != 0}
	}
	return out, nil
}

// ReadFile is Bun.file(path).text().
func ReadFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", Wrap("open", path, err)
	}
	return string(b), nil
}

// WriteFile is Bun.write(path, content), which creates missing parents.
func WriteFile(path, content string) error {
	err := os.WriteFile(path, []byte(content), 0o644)
	if errors.Is(err, fs.ErrNotExist) {
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr == nil {
			err = os.WriteFile(path, []byte(content), 0o644)
		}
	}
	return Wrap("open", path, err)
}

// Exists is Bun.file(path).exists().
func Exists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// MkdirAll is mkdir(path, { recursive: true }).
func MkdirAll(path string) error { return Wrap("mkdir", path, os.MkdirAll(path, 0o755)) }

// RemoveAll is rm(path, { recursive: true, force: true }).
func RemoveAll(path string) error { return Wrap("rm", path, os.RemoveAll(path)) }

// Rename is fs.rename.
func Rename(from, to string) error { return WrapRename(from, to, os.Rename(from, to)) }
