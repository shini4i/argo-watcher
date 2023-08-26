package updater

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMergeParameters(t *testing.T) {
	tests := []struct {
		name     string
		existing ArgoOverrideFile
		new      ArgoOverrideFile
		expected ArgoOverrideFile
	}{
		{
			name:     "Merge with empty existing",
			existing: ArgoOverrideFile{},
			new: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "value1",
						},
					},
				},
			},
			expected: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "value1",
						},
					},
				},
			},
		},
		{
			name: "Overwrite parameter from newContent",
			existing: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "oldValue",
						},
					},
				},
			},
			new: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "newValue",
						},
					},
				},
			},
			expected: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "newValue",
						},
					},
				},
			},
		},
		{
			name: "Append parameter from newContent",
			existing: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "value1",
						},
					},
				},
			},
			new: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param2",
							Value: "value2",
						},
					},
				},
			},
			expected: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "value1",
						},
						{
							Name:  "param2",
							Value: "value2",
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mergeParameters(&test.existing, &test.new)
			assert.Equal(t, test.expected, test.existing)
		})
	}
}
