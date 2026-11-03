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

	_, err = ParseProfile("unknown")
	require.ErrorContains(t, err, "unknown development-network profile")
}

func TestRemoteSignerAuthMode(t *testing.T) {
	require.Equal(t, RemoteSignerAuthToken, ProfileRemoteSigner.RemoteSignerAuthMode())
	require.Equal(t, RemoteSignerAuthTokenMTLS, ProfileRemoteSignerMTLS.RemoteSignerAuthMode())
	require.Equal(t, RemoteSignerAuthToken, ProfileRemoteSignerExits.RemoteSignerAuthMode())
	require.Empty(t, ProfileSingle.RemoteSignerAuthMode())
	require.Empty(t, ProfileAnchored.RemoteSignerAuthMode())
	require.Empty(t, Profile("unknown").RemoteSignerAuthMode())
}

func TestRemoteSignerSignsExits(t *testing.T) {
	require.True(t, ProfileRemoteSignerExits.RemoteSignerSignsExits())
	for _, profile := range []Profile{ProfileRemoteSigner, ProfileRemoteSignerMTLS, ProfileSingle, ProfileAnchored, "unknown"} {
		require.Falsef(t, profile.RemoteSignerSignsExits(), "profile %s", profile)
	}
}
