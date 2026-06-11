package typedids

import "github.com/s22625/orch/internal/model"

func rawConversionsAreBanned() {
	// ruleid: no-raw-model-repo-project-id-conversion
	_ = model.RepoID("https://github.com/acme/repo.git")

	// ruleid: no-raw-model-repo-project-id-conversion
	_ = model.ProjectID("acme-repo")
}

func constructorsAreAllowed() {
	// ok: no-raw-model-repo-project-id-conversion
	repoID, _ := model.NewRepoID("https://github.com/acme/repo.git")
	_ = repoID

	// ok: no-raw-model-repo-project-id-conversion
	projectID, _ := model.NewProjectID("https://github.com/acme/repo.git")
	_ = projectID

	// ok: no-raw-model-repo-project-id-conversion
	_ = model.IssueID("orch-1")
}
