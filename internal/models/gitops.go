package models

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

type GitopsRepo struct {
	RepoUrl    string `validate:"required"`
	BranchName string `validate:"required"`
	Path       string `validate:"required"`
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
	for key, value := range annotations {
		switch key {
		case managedGitRepo:
			gr.RepoUrl = value
		case managedGitBranch:
			gr.BranchName = value
		case managedGitPath:
			gr.Path = value
		}
	}

	validate := validator.New()
	if err := validate.Struct(gr); err != nil {
		return gr, fmt.Errorf("invalid gitops repo: %w", err)
	}

	return gr, nil
}
