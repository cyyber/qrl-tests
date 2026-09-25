package containerfiles

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestArchiveRoundTrip(t *testing.T) {
	archive, err := Archive([]File{
		{Name: "/start.sh", Body: []byte("#!/bin/sh\n"), Mode: 0o755},
		{Name: "keys/keystore.json", Body: []byte("{}")},
		{Name: "/config/../network/config.yaml", Body: []byte("PRESET_BASE: minimal\n")},
	})
	require.NoError(t, err)

	files, err := ReadArchive(bytes.NewReader(archive), "/")
	require.NoError(t, err)
	require.Equal(t, []File{
		{Name: "/start.sh", Body: []byte("#!/bin/sh\n"), Mode: 0o755},
		{Name: "/keys/keystore.json", Body: []byte("{}"), Mode: 0o600},
		{Name: "/network/config.yaml", Body: []byte("PRESET_BASE: minimal\n"), Mode: 0o600},
	}, files)
}

func TestArchiveWritesNoDirectories(t *testing.T) {
	archive, err := Archive([]File{{Name: "/config/network/config.yaml"}})
	require.NoError(t, err)

	reader := tar.NewReader(bytes.NewReader(archive))
	var names []string
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		names = append(names, header.Name)
	}
	require.Equal(t, []string{"config/network/config.yaml"}, names,
		"directory entries would reset existing directories; Docker creates missing parents itself")
}

func TestArchiveRejectsInvalidNames(t *testing.T) {
	for _, test := range []struct {
		name    string
		files   []File
		wantErr string
	}{
		{name: "empty", files: []File{{Name: "/"}}, wantErr: "file name is empty"},
		{
			name:    "listed twice",
			files:   []File{{Name: "/network/config.yaml"}, {Name: "/config/../network/config.yaml"}},
			wantErr: "file /network/config.yaml is listed twice",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Archive(test.files)
			require.EqualError(t, err, test.wantErr)
		})
	}
}

func TestReadArchiveSkipsDirectories(t *testing.T) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	require.NoError(t, writer.WriteHeader(&tar.Header{Name: "keys/", Mode: 0o755, Typeflag: tar.TypeDir}))
	require.NoError(t, writer.WriteHeader(&tar.Header{Name: "keys/keystore.json", Mode: 0o600, Typeflag: tar.TypeReg, Size: 2}))
	_, err := writer.Write([]byte("{}"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	files, err := ReadArchive(&archive, "/data")
	require.NoError(t, err)
	require.Equal(t, []File{{Name: "/data/keys/keystore.json", Body: []byte("{}"), Mode: 0o600}}, files)
}

func TestRead(t *testing.T) {
	// Docker names the entries relative to the source's parent directory.
	client := copierWith(t, File{Name: "keys/a.json", Body: []byte("a")}, File{Name: "keys/b.json", Body: []byte("b")})

	files, err := Read(t.Context(), client, "container", "/data/keys")
	require.NoError(t, err)
	require.Equal(t, []File{
		{Name: "/data/keys/a.json", Body: []byte("a"), Mode: 0o600},
		{Name: "/data/keys/b.json", Body: []byte("b"), Mode: 0o600},
	}, files)
	require.Equal(t, "container", client.containerID)
	require.Equal(t, "/data/keys", client.source)
}

func TestReadFile(t *testing.T) {
	client := copierWith(t, File{Name: "config.yaml", Body: []byte("PRESET_BASE: minimal\n")})

	body, err := ReadFile(t.Context(), client, "container", "/network-configs/config.yaml")
	require.NoError(t, err)
	require.Equal(t, "PRESET_BASE: minimal\n", string(body))
}

func TestReadFileErrors(t *testing.T) {
	for name, test := range map[string]struct {
		client  *copier
		source  string
		wantErr string
	}{
		"copy failure": {
			client:  &copier{err: errors.New("no such file")},
			source:  "/network-configs/config.yaml",
			wantErr: "copy /network-configs/config.yaml: no such file",
		},
		"source is a directory": {
			// Docker archives a directory with its contents, under its base name.
			client:  copierWith(t, File{Name: "network-configs/config.yaml"}),
			source:  "/network-configs",
			wantErr: "archive does not contain /network-configs",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ReadFile(t.Context(), test.client, "container", test.source)
			require.EqualError(t, err, test.wantErr)
		})
	}
}

// copier serves one archive, or an error, and records what was copied.
type copier struct {
	archive []byte
	err     error

	containerID string
	source      string
}

func copierWith(t *testing.T, files ...File) *copier {
	t.Helper()
	archive, err := Archive(files)
	require.NoError(t, err)
	return &copier{archive: archive}
}

func (client *copier) CopyFromContainer(_ context.Context, containerID string, options dockerclient.CopyFromContainerOptions) (dockerclient.CopyFromContainerResult, error) {
	client.containerID = containerID
	client.source = options.SourcePath
	if client.err != nil {
		return dockerclient.CopyFromContainerResult{}, client.err
	}
	return dockerclient.CopyFromContainerResult{Content: io.NopCloser(bytes.NewReader(client.archive))}, nil
}
