package models

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

type GitopsRepo struct {
	RepoUrl    string `validate:"required"`
	BranchName string `validate:"required"`
	Path       string `validate:"required"`
	Filename   string
}

func NewGitopsRepo(app *Application) (GitopsRepo, error) {
	if app.Spec.Sources != nil {
		return extractGitOverrides(app.Metadata.Annotations)
	}
	return GitopsRepo{
		RepoUrl:    app.Spec.Source.RepoURL,
		BranchName: app.Spec.Source.TargetRevision,
		Path:       app.Spec.Source.Path,
	}, nil
}

func extractGitOverrides(annotations map[string]string) (GitopsRepo, error) {
	var gr GitopsRepo
	fields := map[string]*string{
		managedGitRepo:   &gr.RepoUrl,
		managedGitBranch: &gr.BranchName,
		managedGitPath:   &gr.Path,
		managedGitFile:   &gr.Filename,
	}

	for key, value := range annotations {
		if field, ok := fields[key]; ok {
			*field = value
		}
	}

	validate := validator.New()
	if err := validate.Struct(gr); err != nil {
		return gr, fmt.Errorf("invalid gitops repo: %w", err)
	}

	return gr, nil
}
