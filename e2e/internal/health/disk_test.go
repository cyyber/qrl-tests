package health

import (
	"context"
	"errors"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/sidecartest"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestDiskUsage(t *testing.T) {
	const path = "/data/gqrl/execution-data"
	for _, test := range []struct {
		name     string
		output   string
		exitCode int
		want     uint64
		wantErr  string
	}{
		{name: "a total", output: "1536\t" + path + "\n", want: 1536 << 10},
		{
			name:     "a file removed while du walks",
			output:   "du: can't stat '" + path + "/chaindata/000042.sst': No such file or directory\n2048\t" + path + "\n",
			exitCode: 1,
			want:     2048 << 10,
		},
		{
			name:     "a missing directory",
			output:   "du: " + path + ": No such file or directory\n",
			exitCode: 1,
			wantErr:  "du of " + path + " in node: du -sk " + path + ": exit 1",
		},
		{name: "no total", output: "\n", wantErr: "printed no total"},
		{name: "a total that is not a number", output: "many\t" + path + "\n", wantErr: `parsing "many"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			docker.ExecOutput, docker.ExecExitCode = test.output, test.exitCode

			used, err := DiskUsage(t.Context(), docker, "node", path)
			require.Equal(t, [][]string{{"du", "-sk", path}}, docker.Execs)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, used)
		})
	}
}

type sizingDocker struct {
	size    *int64
	err     error
	options dockerclient.ContainerInspectOptions
}

func (docker *sizingDocker) ContainerInspect(
	_ context.Context, _ string, options dockerclient.ContainerInspectOptions,
) (dockerclient.ContainerInspectResult, error) {
	docker.options = options
	return dockerclient.ContainerInspectResult{Container: containertypes.InspectResponse{SizeRw: docker.size}}, docker.err
}

func TestWritableLayerSize(t *testing.T) {
	written := int64(3 << 20)
	docker := &sizingDocker{size: &written}
	size, err := WritableLayerSize(t.Context(), docker, "beacon")
	require.NoError(t, err)
	require.Equal(t, uint64(3<<20), size)
	require.True(t, docker.options.Size, "Docker sizes a container only when asked")

	_, err = WritableLayerSize(t.Context(), &sizingDocker{}, "beacon")
	require.EqualError(t, err, "docker did not size the writable layer of beacon")

	gone := errors.New("no such container")
	_, err = WritableLayerSize(t.Context(), &sizingDocker{err: gone}, "beacon")
	require.ErrorIs(t, err, gone)
}
