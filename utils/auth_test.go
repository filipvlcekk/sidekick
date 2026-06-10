package utils

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/ssh"
)

func TestGetSSHAuthMethodsUsesKeyFilesWithoutAgent(t *testing.T) {
	originalKeyFilesLoader := loadKeyFilesAuthMethods
	originalAgentLoader := loadSSHAgentAuthMethod
	t.Cleanup(func() {
		loadKeyFilesAuthMethods = originalKeyFilesLoader
		loadSSHAgentAuthMethod = originalAgentLoader
	})

	loadKeyFilesAuthMethods = func() ([]ssh.AuthMethod, error) {
		return []ssh.AuthMethod{ssh.Password("keyfile")}, nil
	}
	loadSSHAgentAuthMethod = func(socket string) (ssh.AuthMethod, io.Closer, error) {
		t.Fatalf("agent loader should not be called when socket is empty")
		return nil, nil, nil
	}

	methods, closer, err := getSSHAuthMethods("")

	assert.NoError(t, err)
	assert.Len(t, methods, 1)
	assert.Nil(t, closer)
}

func TestGetSSHAuthMethodsIncludesAgentWhenAvailable(t *testing.T) {
	originalKeyFilesLoader := loadKeyFilesAuthMethods
	originalAgentLoader := loadSSHAgentAuthMethod
	t.Cleanup(func() {
		loadKeyFilesAuthMethods = originalKeyFilesLoader
		loadSSHAgentAuthMethod = originalAgentLoader
	})

	loadKeyFilesAuthMethods = func() ([]ssh.AuthMethod, error) {
		return []ssh.AuthMethod{ssh.Password("keyfile")}, nil
	}
	loadSSHAgentAuthMethod = func(socket string) (ssh.AuthMethod, io.Closer, error) {
		assert.Equal(t, "/tmp/agent.sock", socket)
		return ssh.Password("agent"), io.NopCloser(nil), nil
	}

	methods, closer, err := getSSHAuthMethods("/tmp/agent.sock")

	assert.NoError(t, err)
	assert.Len(t, methods, 2)
	assert.NotNil(t, closer)
	assert.NoError(t, closer.Close())
}

func TestGetSSHAuthMethodsReturnsErrorWithoutAnyMethods(t *testing.T) {
	originalKeyFilesLoader := loadKeyFilesAuthMethods
	originalAgentLoader := loadSSHAgentAuthMethod
	t.Cleanup(func() {
		loadKeyFilesAuthMethods = originalKeyFilesLoader
		loadSSHAgentAuthMethod = originalAgentLoader
	})

	loadKeyFilesAuthMethods = func() ([]ssh.AuthMethod, error) {
		return nil, nil
	}
	loadSSHAgentAuthMethod = func(socket string) (ssh.AuthMethod, io.Closer, error) {
		return nil, nil, errors.New("agent unavailable")
	}

	methods, closer, err := getSSHAuthMethods("/tmp/agent.sock")

	assert.Nil(t, methods)
	assert.Nil(t, closer)
	assert.EqualError(t, err, "no SSH authentication methods available; add a private key under ~/.ssh or start ssh-agent")
}
