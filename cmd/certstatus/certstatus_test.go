package certstatus

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestACMEEntryExists(t *testing.T) {
	acmeJSON := `{"Certificates":[{"domain":{"main":"app.example.com"}},{"domain":{"main":"api.example.com"}}]}`

	assert.True(t, acmeEntryExists(acmeJSON, "app.example.com"))
	assert.False(t, acmeEntryExists(acmeJSON, "worker.example.com"))
	assert.False(t, acmeEntryExists("{}", "app.example.com"))
}

func TestFilterLogsForDomainReturnsLatestRelevantError(t *testing.T) {
	logs := `
time=1 msg="unable to generate a certificate for app.example.com"
time=2 msg="renewed certificate for other.example.com"
time=3 msg="error renewing app.example.com"
`

	assert.Equal(t, `time=3 msg="error renewing app.example.com"`, filterLogsForDomain(logs, "app.example.com"))
	assert.Equal(t, "", filterLogsForDomain(logs, "worker.example.com"))
}
