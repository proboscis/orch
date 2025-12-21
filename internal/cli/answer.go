package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/s22625/orch/internal/model"
	"github.com/spf13/cobra"
)

type answerOptions struct {
	Text string
	File string
	By   string
}

func newAnswerCmd() *cobra.Command {
	opts := &answerOptions{}

	cmd := &cobra.Command{
		Use:   "answer RUN_REF QUESTION_ID",
		Short: "Answer a question",
		Long: `Answer a question event in a run.

This appends an answer event to the run, which can unblock it
for the next tick.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnswer(args[0], args[1], opts)
		},
	}

	cmd.Flags().StringVar(&opts.Text, "text", "", "Answer text")
	cmd.Flags().StringVar(&opts.File, "file", "", "Read answer from file")
	cmd.Flags().StringVar(&opts.By, "by", "user", "Who is answering (user|system)")

	return cmd
}

func runAnswer(refStr, questionID string, opts *answerOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	// Resolve by short ID or run ref
	run, err := resolveRun(st, refStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	// Get answer text
	answerText := opts.Text
	if opts.File != "" {
		content, err := os.ReadFile(opts.File)
		if err != nil {
			return fmt.Errorf("failed to read answer file: %w", err)
		}
		answerText = string(content)
	}

	if answerText == "" {
		return fmt.Errorf("answer text is required (use --text or --file)")
	}

	// Check if question exists (optional validation)
	found := false
	for _, q := range run.UnansweredQuestions() {
		if q.Name == questionID {
			found = true
			break
		}
	}

	if !found {
		// Still allow answering even if question not found (might be already answered)
		// but warn the user
		if !globalOpts.Quiet {
			fmt.Fprintf(os.Stderr, "warning: question %s not found in unanswered questions\n", questionID)
		}
	}

	// Append answer event
	event := model.NewAnswerEvent(questionID, answerText, opts.By)
	if err := st.AppendEvent(run.Ref(), event); err != nil {
		return fmt.Errorf("failed to append answer: %w", err)
	}

	// Output
	if globalOpts.JSON {
		output := struct {
			OK         bool   `json:"ok"`
			IssueID    string `json:"issue_id"`
			RunID      string `json:"run_id"`
			QuestionID string `json:"question_id"`
			By         string `json:"by"`
		}{
			OK:         true,
			IssueID:    run.IssueID,
			RunID:      run.RunID,
			QuestionID: questionID,
			By:         opts.By,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	if !globalOpts.Quiet {
		fmt.Printf("Answered question %s in %s#%s\n", questionID, run.IssueID, run.RunID)
	}

	return nil
}
