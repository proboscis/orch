//go:build semgrepfixture

// Semgrep test fixture for ADR-0004 R2. Parsed by `semgrep test`, never compiled.
package fixture

func badEditStorePath(issue *Issue) error {
	// ruleid: adr0004-cli-no-editor-on-store-path
	return openInEditor(issue.Path)
}

func okEditTemporaryCopy(issue *Issue) error {
	tmp := writeTempCopy(issue)
	// ok: adr0004-cli-no-editor-on-store-path
	if err := openInEditor(tmp); err != nil {
		return err
	}
	return submitIssueUpdate(issue.ID, tmp)
}
