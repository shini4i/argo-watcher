package models

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

type GitopsRepo struct {
	RepoUrl       string `validate:"required"`
	BranchName    string `validate:"required"`
	Path          string `validate:"required"`
	Filename      string
	RepoCachePath string
}

func NewGitopsRepo(app *Application, repoCachePath string) (GitopsRepo, error) {
	gitopsRepo, err := extractGitOverrides(app.Metadata.Annotations, app, app.Spec.Sources == nil)
	if err != nil {
		return gitopsRepo, err
	}
	gitopsRepo.RepoCachePath = repoCachePath
	return gitopsRepo, nil
}

func extractGitOverrides(annotations map[string]string, app *Application, isSourceNil bool) (GitopsRepo, error) {
	gr := GitopsRepo{
		RepoUrl:    app.Spec.Source.RepoURL,
		BranchName: app.Spec.Source.TargetRevision,
		Path:       app.Spec.Source.Path,
	}

	fields := make(map[string]*string)

	if isSourceNil {
		// when app.Spec.Sources is nil, just include the managedGitFile
		fields[managedGitFile] = &gr.Filename
	} else {
		// when app.Spec.Sources is not nil, include all fields
		fields = map[string]*string{
			managedGitRepo:   &gr.RepoUrl,
			managedGitBranch: &gr.BranchName,
			managedGitPath:   &gr.Path,
			managedGitFile:   &gr.Filename,
		}
	}

	for key, value := range annotations {
		if field, ok := fields[key]; ok {
			*field = value
		}
	}

	// Perform struct validation
	validate := validator.New()
	if err := validate.Struct(gr); err != nil {
		return gr, fmt.Errorf("invalid gitops repo: %w", err)
	}

	return gr, nil
}
