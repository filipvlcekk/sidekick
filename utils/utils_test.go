package utils

import (
	"crypto/md5"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
)

func TestHandleEnvFile(t *testing.T) {
	envFileName := "test.env"
	dockerEnvProperty := []string{}
	var envFileChecksum string
	var gotPublicKey string
	var gotEnvFileName string
	var gotOutputFile string

	envContent := "KEY1=value1\nKEY2=value2"

	err := os.WriteFile(envFileName, []byte(envContent), 0644)
	assert.NoError(t, err)
	defer os.Remove(envFileName)
	defer os.Remove("encrypted.env")
	envFile, envFileErr := os.Open(fmt.Sprintf("./%s", envFileName))
	assert.NoError(t, envFileErr)
	defer envFile.Close()

	envMap, envParseErr := godotenv.Parse(envFile)
	assert.NoError(t, envParseErr)
	envFileContent, envMarshalErr := godotenv.Marshal(envMap)
	assert.NoError(t, envMarshalErr)

	pub := "age1lgjx644dkpj2nas84pfe4dsd96tph8yxhgf6zfh58kqw06qycavsz00rzm"
	err = handleEnvFile(envFileName, &dockerEnvProperty, &envFileChecksum, pub, func(publicKey, envFileName, outputFile string) error {
		gotPublicKey = publicKey
		gotEnvFileName = envFileName
		gotOutputFile = outputFile
		return os.WriteFile(outputFile, []byte("KEY1=value1\nKEY2=value2\n"), 0644)
	})
	assert.NoError(t, err)

	assert.Contains(t, dockerEnvProperty, "KEY2=${KEY2}")
	assert.Contains(t, dockerEnvProperty, "KEY1=${KEY1}")

	expectedChecksum := fmt.Sprintf("%x", md5.Sum([]byte(envFileContent)))
	assert.Equal(t, expectedChecksum, envFileChecksum)
	assert.Equal(t, pub, gotPublicKey)
	assert.Equal(t, "./test.env", gotEnvFileName)
	assert.Equal(t, "encrypted.env", gotOutputFile)
}

func TestHandleEnvFile_ReturnsSopsRunnerError(t *testing.T) {
	envFileName := "test.env"
	dockerEnvProperty := []string{}
	var envFileChecksum string

	err := os.WriteFile(envFileName, []byte("KEY1=value1\n"), 0644)
	assert.NoError(t, err)
	defer os.Remove(envFileName)
	defer os.Remove("encrypted.env")

	expectedErr := errors.New("sops runner failed")
	err = handleEnvFile(envFileName, &dockerEnvProperty, &envFileChecksum, "age1test", func(publicKey, envFileName, outputFile string) error {
		return expectedErr
	})
	assert.ErrorIs(t, err, expectedErr)
}

func TestLoadAppConfig(t *testing.T) {
	configContent := `
name: test
version: V1
image: ""
url: MOCK_URL
port: 3000
createdAt: Mon Nov 11 21:42:50 KST 2024
`
	err := os.WriteFile("sidekick.yml", []byte(configContent), 0644)
	assert.NoError(t, err)
	defer os.Remove("sidekick.yml")

	appConfig, err := LoadAppConfig()
	assert.NoError(t, err)

	assert.Equal(t, "test", appConfig.Name)
	assert.Equal(t, "V1", appConfig.Version)
	assert.Equal(t, "MOCK_URL", appConfig.Url)
}

func TestLoadAppConfig_FileNotFound(t *testing.T) {
	os.Remove("sidekick.yml")

	_, err := LoadAppConfig()
	assert.Error(t, err)
	assert.Equal(t, "Sidekick app config not found. Please run sidekick launch first", err.Error())
}
