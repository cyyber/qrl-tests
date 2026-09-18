package depositcli

import (
	"archive/tar"
	"bytes"
	"fmt"

	"github.com/cyyber/qrl-tests/e2e/internal/operatorvc"
)

func writeRegularFiles(files []operatorvc.File) ([]byte, error) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, file := range files {
		header := &tar.Header{
			Name:     file.Name,
			Mode:     0o600,
			Typeflag: tar.TypeReg,
			Size:     int64(len(file.Body)),
		}
		if file.Name == "run-deposit.sh" {
			header.Mode = 0o755
		}
		if err := writer.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("archive %s: %w", file.Name, err)
		}
		if _, err := writer.Write(file.Body); err != nil {
			return nil, fmt.Errorf("archive %s: %w", file.Name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("archive deposit CLI fixtures: %w", err)
	}
	return archive.Bytes(), nil
}
