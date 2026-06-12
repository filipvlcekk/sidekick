package launch

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mightymoud/sidekick/render"
	"github.com/mightymoud/sidekick/utils"
	"github.com/stretchr/testify/assert"
)

func TestServiceDirCreateCmdUsesMkdirP(t *testing.T) {
	assert.Equal(t, "mkdir -p uptimekuma", serviceDirCreateCmd("uptimekuma"))
}

func TestShouldValidateTLSRequiresSuccessfulLaunch(t *testing.T) {
	tests := []struct {
		name     string
		model    tea.Model
		expected bool
	}{
		{
			name:     "successful launch",
			model:    render.TuiModel{AllDone: true},
			expected: true,
		},
		{
			name:     "failed launch",
			model:    render.TuiModel{AllDone: false},
			expected: false,
		},
		{
			name:     "unexpected model type",
			model:    nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shouldValidateTLS(tt.model))
		})
	}
}

func TestBuildDockerServiceIncludesPerAppCertresolverInWildcardMode(t *testing.T) {
	service, err := buildDockerService(
		utils.SidekickServer{
			CertificateMode: utils.CertificateModeWildcard,
			WildcardDomain:  "saola.cz",
		},
		"uptimekuma",
		"uptimekuma.saola.cz",
		"3001",
		"uptimekuma",
		nil,
	)

	assert.NoError(t, err)
	assert.Contains(t, service.Labels, "traefik.http.routers.uptimekuma.tls=true")
	assert.Contains(t, service.Labels, "traefik.http.routers.uptimekuma.tls.certresolver=default")
}

func TestBuildDockerServiceRejectsOutOfZoneWildcardDomain(t *testing.T) {
	_, err := buildDockerService(
		utils.SidekickServer{
			CertificateMode: utils.CertificateModeWildcard,
			WildcardDomain:  "saola.cz",
		},
		"uptimekuma",
		"foo.example.com",
		"3001",
		"uptimekuma",
		nil,
	)

	assert.EqualError(t, err, "app domain foo.example.com is outside wildcard domain saola.cz")
}
