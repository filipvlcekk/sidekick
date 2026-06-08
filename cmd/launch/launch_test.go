package launch

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mightymoud/sidekick/render"
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
