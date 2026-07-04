// Command automem is a zero-config, offline, single-binary persistent-memory
// layer for coding-agent CLIs. It captures an extractive summary of each agent
// session into an append-only JSONL store and recalls the most relevant prior
// summaries — lexical overlap times recency decay, no vector DB, no account,
// no API key, no daemon.
//
// This file wires the command tree onto a cobra root. The m1 commands
// (capture, recall, stats) are backed by the internal/* packages; install wires
// automem into the supported agents, and sync/team are the paid-tier stubs.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/automem/internal/capture"
	"github.com/SuperMarioYL/automem/internal/install"
	"github.com/SuperMarioYL/automem/internal/recall"
	"github.com/SuperMarioYL/automem/internal/stats"
	"github.com/SuperMarioYL/automem/internal/store"
)

// paidTierHint is the message the sync/team paid-tier stubs return. The local
// substrate works fully offline without them; cross-machine sync and shared
// team spaces are the one thing automem deliberately does not do for free.
const paidTierHint = "requires automem cloud — see lei6393.com/automem"

// version is the semantic version of the binary. It is overridden at release
// time via -ldflags "-X main.version=<tag>"; the default mirrors the VERSION
// file so a `go build` from source still reports something sensible.
var version = "0.1.0"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "automem:", err)
		os.Exit(1)
	}
}

// newRootCmd builds the top-level `automem` command and attaches every
// subcommand. Keeping construction in a function (rather than a package-level
// var) makes the tree testable and keeps global state out of the binary.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "automem",
		Short: "Offline memory layer so your coding agent remembers across restarts",
		Long: "automem is a single offline binary that gives any supported coding-agent CLI\n" +
			"persistent cross-session memory. It captures an extractive summary of each\n" +
			"session into ~/.automem/store.jsonl and recalls the most relevant prior\n" +
			"summaries by lexical overlap times recency decay — no vector DB, no account,\n" +
			"no API key, no daemon.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newCaptureCmd(),
		newRecallCmd(),
		newStatsCmd(),
		newInstallCmd(),
		newSyncCmd(),
		newTeamCmd(),
	)

	return root
}

// openStore opens the default store (~/.automem, or $AUTOMEM_DIR). Every m1
// command funnels through here so the store location is resolved in exactly one
// place.
func openStore() (*store.Store, error) {
	return store.OpenDefault()
}

// readTranscript returns the transcript source for capture: the named file if
// given, otherwise stdin. The caller closes nothing — stdin isn't ours to close
// and files are wrapped to close on their own.
func readTranscript(cmd *cobra.Command, args []string) (io.Reader, func() error, error) {
	if len(args) == 1 && args[0] != "-" {
		f, err := os.Open(args[0])
		if err != nil {
			return nil, nil, fmt.Errorf("open transcript %s: %w", args[0], err)
		}
		return f, f.Close, nil
	}
	return cmd.InOrStdin(), func() error { return nil }, nil
}

func newCaptureCmd() *cobra.Command {
	var agent string
	var cwd string
	var maxMsgs int

	cmd := &cobra.Command{
		Use:   "capture [transcript]",
		Short: "Append an extractive memory record from an agent session transcript",
		Long: "Read a session transcript (from the given file or stdin), extract a\n" +
			"deterministic summary — last-N user messages, touched paths, diff stat —\n" +
			"and append one record to ~/.automem/store.jsonl. No AI key, no network.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, closeSrc, err := readTranscript(cmd, args)
			if err != nil {
				return err
			}
			defer closeSrc()

			transcript, err := capture.ParseTranscript(src)
			if err != nil {
				return err
			}

			if cwd == "" {
				// Best-effort: record where the session ran.
				cwd, _ = os.Getwd()
			}
			rec := capture.Extract(transcript, capture.Options{
				Agent:       agent,
				Cwd:         cwd,
				MaxUserMsgs: maxMsgs,
			})

			st, err := openStore()
			if err != nil {
				return err
			}
			stored, err := st.Append(rec)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "captured %s (%d tag(s))\n", stored.ID, len(stored.Tags))
			return nil
		},
	}

	cmd.Flags().StringVar(&agent, "agent", "", `capturing agent label ("claude-code" | "aider")`)
	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory to record (default: current dir)")
	cmd.Flags().IntVar(&maxMsgs, "max-msgs", 0, "max trailing user messages to keep in the summary (0 = default)")
	return cmd
}

func newRecallCmd() *cobra.Command {
	var topK int
	var noMark bool

	cmd := &cobra.Command{
		Use:   "recall [query]",
		Short: "Print the top-K prior summaries most relevant to a query",
		Long: "Score every stored record by lexical overlap with the query times a\n" +
			"recency-decay kernel and print the top-K summaries, incrementing each\n" +
			"surfaced record's injected counter (the is-it-used signal).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := recallQuery(cmd, args)
			if err != nil {
				return err
			}

			st, err := openStore()
			if err != nil {
				return err
			}
			records, err := st.Load()
			if err != nil {
				return err
			}

			results := recall.Recall(records, query, recall.Options{TopK: topK})
			if len(results) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no relevant memories)")
				return nil
			}

			out := cmd.OutOrStdout()
			for i, r := range results {
				fmt.Fprintf(out, "# memory %d/%d  (score %.3f)\n", i+1, len(results), r.Score)
				fmt.Fprintln(out, r.Record.Summary)
				if i < len(results)-1 {
					fmt.Fprintln(out)
				}
			}

			// Mark surfaced records injected — the is-it-used signal. Skippable
			// so a dry preview doesn't pollute the counter.
			if !noMark {
				if _, err := st.MarkInjected(recall.IDs(results)); err != nil {
					return err
				}
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&topK, "top", "k", recall.DefaultTopK, "number of memories to surface")
	cmd.Flags().BoolVar(&noMark, "no-mark", false, "don't increment the injected counter (dry preview)")
	return cmd
}

// recallQuery returns the recall query from the positional arg, or from stdin
// when no arg is given (so a hook can pipe the incoming prompt in).
func recallQuery(cmd *cobra.Command, args []string) (string, error) {
	if len(args) == 1 && args[0] != "-" {
		return args[0], nil
	}
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", fmt.Errorf("read query from stdin: %w", err)
	}
	return string(data), nil
}

func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Print stored-vs-injected memory counts",
		Long: "Report how many records are stored and how many have been injected into\n" +
			"a later session — proof the memory is actually used, not just written.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			records, err := st.Load()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), stats.Compute(records).Format())
			return nil
		},
	}
}

func newInstallCmd() *cobra.Command {
	var dryRun bool
	var force bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Auto-wire supported coding agents (Claude Code, Aider)",
		Long: "Detect installed coding agents and wire automem into them: Claude Code\n" +
			"SessionStart/Stop hooks and an Aider wrapper on macOS and Linux. No manual\n" +
			"config — the next session already remembers the last.\n\n" +
			"The Aider wrapper is shipped best-effort and marked unverified; the Claude\n" +
			"Code hooks are the verified path.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			results, err := install.Run(install.Config{DryRun: dryRun, Force: force})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, install.Report(results))
			if detail := install.Detail(results); detail != "" {
				fmt.Fprintln(out, detail)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be wired without writing any file")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing automem wiring instead of leaving it alone")
	return cmd
}

// newSyncCmd and newTeamCmd are the paid-tier stubs. They exist so the CLI
// surface is complete and so inbound interest is measurable (each invocation is
// a demand signal), but the feature itself is hosted and paid — the local
// substrate works fully offline without either.

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Cross-machine sync (paid tier)",
		Long: "Cross-machine memory sync is a hosted paid-tier feature. The local\n" +
			"substrate works fully offline without it.",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), paidTierHint)
			return nil
		},
	}
}

func newTeamCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "team",
		Short: "Shared team memory spaces (paid tier)",
		Long: "Shared team scopes with cross-machine sync and an audit log are a hosted\n" +
			"paid-tier feature on top of the free offline substrate.",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), paidTierHint)
			return nil
		},
	}
}
