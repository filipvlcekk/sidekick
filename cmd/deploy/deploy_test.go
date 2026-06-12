package deploy

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mightymoud/sidekick/render"
	"github.com/mightymoud/sidekick/utils"
	"github.com/stretchr/testify/assert"
)

func TestBuildDockerComposeFileUsesAppConfigPortAndDomain(t *testing.T) {
	appConfig := utils.SidekickAppConfig{
		Name: "uptimekuma",
		Url:  "uptimekuma.saola.cz",
		Port: 3001,
	}

	compose, err := buildDockerComposeFile(appConfig, []string{"SECRET=${SECRET}"}, utils.SidekickServer{})
	assert.NoError(t, err)
	service := compose.Services["uptimekuma"]

	assert.Contains(t, service.Labels, "traefik.http.routers.uptimekuma.rule=Host(`uptimekuma.saola.cz`)")
	assert.Contains(t, service.Labels, "traefik.http.services.uptimekuma.loadbalancer.server.port=3001")
	assert.Contains(t, service.Labels, "traefik.http.routers.uptimekuma.tls.certresolver=default")
	assert.Contains(t, service.Environment, "SECRET=${SECRET}")
	assert.Equal(t, []string{"sidekick"}, service.Networks)
}

func TestBuildDockerComposeFileIncludesPerAppCertresolverInWildcardMode(t *testing.T) {
	appConfig := utils.SidekickAppConfig{
		Name: "uptimekuma",
		Url:  "uptimekuma.saola.cz",
		Port: 3001,
	}

	compose, err := buildDockerComposeFile(appConfig, nil, utils.SidekickServer{
		CertificateMode: utils.CertificateModeWildcard,
		WildcardDomain:  "saola.cz",
	})
	assert.NoError(t, err)

	service := compose.Services["uptimekuma"]
	assert.Contains(t, service.Labels, "traefik.http.routers.uptimekuma.tls=true")
	assert.Contains(t, service.Labels, "traefik.http.routers.uptimekuma.tls.certresolver=default")
}

func TestBuildDockerComposeFileRejectsOutOfZoneWildcardDomain(t *testing.T) {
	appConfig := utils.SidekickAppConfig{
		Name: "uptimekuma",
		Url:  "foo.example.com",
		Port: 3001,
	}

	_, err := buildDockerComposeFile(appConfig, nil, utils.SidekickServer{
		CertificateMode: utils.CertificateModeWildcard,
		WildcardDomain:  "saola.cz",
	})

	assert.EqualError(t, err, "app domain foo.example.com is outside wildcard domain saola.cz")
}

func TestShouldValidateTLSRequiresSuccessfulDeploy(t *testing.T) {
	tests := []struct {
		name     string
		model    tea.Model
		expected bool
	}{
		{
			name:     "successful deploy",
			model:    render.TuiModel{AllDone: true},
			expected: true,
		},
		{
			name:     "failed deploy",
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
