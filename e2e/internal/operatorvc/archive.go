package operatorvc

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// File is one regular file placed in a sidecar fixture archive.
type File struct {
	Name string
	Body []byte
}

func fixtureArchive(genesisConfig []byte, keystores []File) ([]byte, error) {
	if len(genesisConfig) == 0 {
		return nil, errors.New("genesis config is empty")
	}
	files := []tarFile{
		{Name: strings.TrimPrefix(startScriptPath, "/"), Mode: 0o755, Body: []byte(startScript)},
		{Name: strings.TrimPrefix(passwordPath, "/"), Mode: 0o600, Body: []byte(walletPassword)},
		{Name: "network-configs", Mode: 0o755, Directory: true},
		{Name: strings.TrimPrefix(configPath, "/"), Mode: 0o600, Body: genesisConfig},
	}
	if len(keystores) > 0 {
		files = append(files, tarFile{Name: "keys", Mode: 0o755, Directory: true})
		for _, keystore := range keystores {
			name := path.Base(strings.TrimSpace(keystore.Name))
			if name == "" || name == "." || name == "/" {
				return nil, errors.New("keystore name is empty")
			}
			files = append(files, tarFile{
				Name: path.Join("keys", name),
				Mode: 0o600,
				Body: keystore.Body,
			})
		}
	}
	return tarFiles(files)
}

type tarFile struct {
	Name      string
	Mode      int64
	Body      []byte
	Directory bool
}

func tarFiles(files []tarFile) ([]byte, error) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, file := range files {
		header := &tar.Header{Name: file.Name, Mode: file.Mode}
		if file.Directory {
			header.Typeflag = tar.TypeDir
		} else {
			header.Typeflag = tar.TypeReg
			header.Size = int64(len(file.Body))
		}
		if err := writer.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("archive %s: %w", file.Name, err)
		}
		if file.Directory {
			continue
		}
		if _, err := writer.Write(file.Body); err != nil {
			return nil, fmt.Errorf("archive %s: %w", file.Name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("archive operator validator fixtures: %w", err)
	}
	return archive.Bytes(), nil
}

func readTarFile(reader io.Reader, want string) ([]byte, error) {
	archive := tar.NewReader(reader)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("archive does not contain %s", want)
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if path.Base(header.Name) != want {
			continue
		}
		return io.ReadAll(archive)
	}
}

func parseAuthToken(raw []byte) (string, error) {
	var token string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		token = line
	}
	if token == "" {
		return "", errors.New("validator auth token is empty")
	}
	return token, nil
}
