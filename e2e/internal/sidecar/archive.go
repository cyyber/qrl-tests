package sidecar

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const defaultFileMode = 0o600

// File is one regular file copied into or out of a sidecar container.
type File struct {
	// Name is the file's path inside the container. A relative name is taken
	// from the root.
	Name string
	Body []byte
	// Mode holds the permission bits; zero means 0600.
	Mode int64
}

// archiveFiles tars files for extraction at the container root. The files are
// owned by root.
//
// It writes no directory entries, because Docker would apply their mode and
// owner to directories the image already has, such as /tmp. Docker creates
// missing parent directories itself.
func archiveFiles(files []File) ([]byte, error) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, file := range files {
		name := strings.TrimPrefix(path.Clean("/"+file.Name), "/")
		if name == "" {
			return nil, errors.New("file name is empty")
		}

		mode := file.Mode
		if mode == 0 {
			mode = defaultFileMode
		}
		header := &tar.Header{Name: name, Mode: mode, Typeflag: tar.TypeReg, Size: int64(len(file.Body))}
		if err := writer.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("archive %s: %w", name, err)
		}
		if _, err := writer.Write(file.Body); err != nil {
			return nil, fmt.Errorf("archive %s: %w", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return archive.Bytes(), nil
}

// readTarFiles returns the regular files in an archive, each named by joining
// parent and its entry name. Directories and other entry types are skipped.
func readTarFiles(reader io.Reader, parent string) ([]File, error) {
	archive := tar.NewReader(reader)
	var files []File
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		body, err := io.ReadAll(archive)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", header.Name, err)
		}
		files = append(files, File{Name: path.Join(parent, header.Name), Body: body, Mode: header.Mode})
	}
}
