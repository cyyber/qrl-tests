package sidecar

import (
	"archive/tar"
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchiveRoundTrip(t *testing.T) {
	archive, err := archiveFiles([]File{
		{Name: "/start.sh", Body: []byte("#!/bin/sh\n"), Mode: 0o755},
		{Name: "keys/keystore.json", Body: []byte("{}")},
		{Name: "/config/../network/config.yaml", Body: []byte("PRESET_BASE: minimal\n")},
	})
	require.NoError(t, err)

	files, err := readArchive(bytes.NewReader(archive), "/")
	require.NoError(t, err)
	require.Equal(t, []File{
		{Name: "/start.sh", Body: []byte("#!/bin/sh\n"), Mode: 0o755},
		{Name: "/keys/keystore.json", Body: []byte("{}"), Mode: 0o600},
		{Name: "/network/config.yaml", Body: []byte("PRESET_BASE: minimal\n"), Mode: 0o600},
	}, files)
}

func TestArchiveFilesRejectsInvalidNames(t *testing.T) {
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
			_, err := archiveFiles(test.files)
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

	files, err := readArchive(&archive, "/data")
	require.NoError(t, err)
	require.Equal(t, []File{{Name: "/data/keys/keystore.json", Body: []byte("{}"), Mode: 0o600}}, files)
}
