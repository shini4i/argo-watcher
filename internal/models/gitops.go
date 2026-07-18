package models

import (
	"fmt"
	"strings"
)

type GitopsRepo struct {
	RepoUrl       string
	BranchName    string
	Path          string
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

	// RepoUrl, BranchName and Path are mandatory: they come either from the
	// Application's source spec or the managed-git annotations, and a git
	// write-back cannot proceed without all three.
	var missing []string
	if gr.RepoUrl == "" {
		missing = append(missing, "RepoUrl")
	}
	if gr.BranchName == "" {
		missing = append(missing, "BranchName")
	}
	if gr.Path == "" {
		missing = append(missing, "Path")
	}
	if len(missing) > 0 {
		return gr, fmt.Errorf("invalid gitops repo: missing required field(s): %s", strings.Join(missing, ", "))
	}

	return gr, nil
}
