package devnet

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseProfile(t *testing.T) {
	profile, err := ParseProfile("")
	require.NoError(t, err)
	require.Equal(t, ProfileSingle, profile)

	profile, err = ParseProfile("single")
	require.NoError(t, err)
	require.Equal(t, ProfileSingle, profile)

	profile, err = ParseProfile("  single  ")
	require.NoError(t, err)
	require.Equal(t, ProfileSingle, profile)

	profile, err = ParseProfile("   ")
	require.NoError(t, err)
	require.Equal(t, ProfileSingle, profile)

	profile, err = ParseProfile("anchored")
	require.NoError(t, err)
	require.Equal(t, ProfileAnchored, profile)

	profile, err = ParseProfile("remote-signer")
	require.NoError(t, err)
	require.Equal(t, ProfileRemoteSigner, profile)

	profile, err = ParseProfile("remote-signer-mtls")
	require.NoError(t, err)
	require.Equal(t, ProfileRemoteSignerMTLS, profile)

	profile, err = ParseProfile("remote-signer-exits")
	require.NoError(t, err)
	require.Equal(t, ProfileRemoteSignerExits, profile)

	profile, err = ParseProfile("remote-signer-execution")
	require.NoError(t, err)
	require.Equal(t, ProfileRemoteSignerExecution, profile)

	_, err = ParseProfile("unknown")
	require.ErrorContains(t, err, "unknown development-network profile")
}

func TestRemoteSignerAuthMode(t *testing.T) {
	require.Equal(t, RemoteSignerAuthToken, ProfileRemoteSigner.RemoteSignerAuthMode())
	require.Equal(t, RemoteSignerAuthTokenMTLS, ProfileRemoteSignerMTLS.RemoteSignerAuthMode())
	require.Equal(t, RemoteSignerAuthToken, ProfileRemoteSignerExits.RemoteSignerAuthMode())
	require.Empty(t, ProfileSingle.RemoteSignerAuthMode())
	require.Empty(t, ProfileAnchored.RemoteSignerAuthMode())
	// Its signer fronts the execution client, not the validator client.
	require.Empty(t, ProfileRemoteSignerExecution.RemoteSignerAuthMode())
	require.Empty(t, Profile("unknown").RemoteSignerAuthMode())
}

func TestRemoteSignerSignsExits(t *testing.T) {
	require.True(t, ProfileRemoteSignerExits.RemoteSignerSignsExits())
	for _, profile := range []Profile{
		ProfileRemoteSigner, ProfileRemoteSignerMTLS, ProfileRemoteSignerExecution, ProfileSingle, ProfileAnchored, "unknown",
	} {
		require.Falsef(t, profile.RemoteSignerSignsExits(), "profile %s", profile)
	}
}

func TestRunsExecutionSigner(t *testing.T) {
	require.True(t, ProfileRemoteSignerExecution.RunsExecutionSigner())
	for _, profile := range []Profile{
		ProfileRemoteSigner, ProfileRemoteSignerMTLS, ProfileRemoteSignerExits, ProfileSingle, ProfileAnchored, "unknown",
	} {
		require.Falsef(t, profile.RunsExecutionSigner(), "profile %s", profile)
	}
}
