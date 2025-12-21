package cli

import (
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/pr"
)

// populatePRUrls wraps the pr package for backward compatibility.
func populatePRUrls(runs []*model.Run) {
	pr.PopulateRunInfo(runs)
}
