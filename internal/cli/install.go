package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/gray0128/memctl/internal/adapters"
	dshplugin "github.com/gray0128/memctl/plugins/dsh"
	piplugin "github.com/gray0128/memctl/plugins/pi"
)

// runInstall implements `install [agent...] --all --dry-run --status`. It reads
// and merges host configuration only; it never touches the memory repository or
// the Git working tree. See docs/04-ui-and-cli-specification.md section 1.
func runInstall(args []string, stdout, stderr io.Writer) int {
	all := false
	dryRun := false
	statusOnly := false
	var agents []string
	for _, arg := range args {
		switch {
		case arg == "--all":
			all = true
		case arg == "--dry-run":
			dryRun = true
		case arg == "--status":
			statusOnly = true
		case strings.HasPrefix(arg, "-"):
			return usageError(stderr, "install", fmt.Sprintf("unknown flag %q", arg))
		default:
			agents = append(agents, arg)
		}
	}
	switch {
	case statusOnly && (dryRun || all || len(agents) > 0):
		return usageError(stderr, "install", "--status cannot be combined with --all, --dry-run, or agent names")
	case !statusOnly && all && len(agents) > 0:
		return usageError(stderr, "install", "--all cannot be combined with explicit agent names")
	case !statusOnly && !all && len(agents) == 0:
		return usageError(stderr, "install", "specify at least one agent or --all")
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "install", err)
	}
	env := adapters.DefaultEnv(cfg.StateDir)

	if statusOnly {
		return runInstallStatus(env, stdout, stderr)
	}

	pending, err := env.PendingRecovery()
	if err != nil {
		return fail(stderr, "install", err)
	}
	if dryRun {
		if pending {
			fmt.Fprintln(stdout, "pending recovery: an earlier install was interrupted; run install without --dry-run to recover it")
		}
	} else {
		// Serialize recovery and every write with the install-level lock so a
		// concurrent CLI or UI install cannot recover this journal mid-write.
		held, err := env.AcquireInstallLock(context.Background())
		if err != nil {
			return fail(stderr, "install", err)
		}
		defer held.Release()
		recovery, err := env.Recover()
		if err != nil {
			return fail(stderr, "install", err)
		}
		printRecovery(stdout, recovery)
	}

	for _, agent := range agents {
		if _, ok := adapters.Lookup(agent); !ok {
			return usageError(stderr, "install", fmt.Sprintf("unknown adapter %q", agent))
		}
	}

	plans, err := env.PlanAll()
	if err != nil {
		return fail(stderr, "install", err)
	}
	selected := make(map[string]bool)
	for _, agent := range agents {
		selected[agent] = true
	}
	for _, plan := range plans {
		if len(agents) > 0 && !selected[plan.Agent] {
			continue
		}
		printPlan(stdout, plan)
		if dryRun {
			continue
		}
		if !plan.Applicable {
			continue
		}
		if all && !plan.HostFound {
			continue
		}
		result, applyErr := env.Apply(plan)
		printApplyResult(stdout, result)
		if applyErr != nil {
			return fail(stderr, "install", applyErr)
		}
	}
	return ExitOK
}

// runInstallStatus prints the current status of every adapter without writing.
func runInstallStatus(env adapters.Env, stdout, stderr io.Writer) int {
	pending, err := env.PendingRecovery()
	if err != nil {
		return fail(stderr, "install", err)
	}
	if pending {
		fmt.Fprintln(stdout, "pending recovery: an earlier install was interrupted; run install to recover it")
	}
	plans, err := env.PlanAll()
	if err != nil {
		return fail(stderr, "install", err)
	}
	for _, plan := range plans {
		printPlan(stdout, plan)
	}
	return ExitOK
}

func printPlan(stdout io.Writer, plan adapters.InstallPlan) {
	header := fmt.Sprintf("%s: %s", plan.Agent, plan.Status)
	if plan.HostFound {
		header += " host=" + plan.HostCommand
		if plan.HostVersion != "" {
			header += "@" + plan.HostVersion
		}
	} else {
		header += " host=not-detected"
	}
	if plan.PluginVersion != "" {
		header += " plugin@" + plan.PluginVersion
	}
	fmt.Fprintln(stdout, header)
	if plan.Reason != "" {
		fmt.Fprintf(stdout, "  note: %s\n", plan.Reason)
	}
	printDshHints(stdout, plan)
	printPiHints(stdout, plan)
	for _, artifact := range plan.Artifacts {
		path := artifact.Path
		fmt.Fprintf(stdout, "  %s (%s) %s\n", path, artifact.Format, artifact.Action)
		if artifact.Note != "" {
			fmt.Fprintf(stdout, "    note: %s\n", artifact.Note)
		}
		if artifact.Patch != "" {
			if artifact.Action == adapters.ActionManual {
				// A manual patch is a snippet the operator pastes, not a diff, so
				// every line is printed instead of filtering for changed lines.
				fmt.Fprintln(stdout, "    patch:")
				for _, line := range strings.Split(strings.TrimSuffix(artifact.Patch, "\n"), "\n") {
					fmt.Fprintf(stdout, "      %s\n", line)
				}
			} else {
				for _, line := range strings.Split(strings.TrimSuffix(artifact.Patch, "\n"), "\n") {
					if line == "" {
						continue
					}
					if line[0] == ' ' {
						continue
					}
					fmt.Fprintf(stdout, "    %s\n", line)
				}
			}
		}
	}
}

func printApplyResult(stdout io.Writer, result adapters.ApplyResult) {
	for _, applied := range result.Applied {
		line := fmt.Sprintf("  applied %s %s", applied.Path, applied.Action)
		if applied.BackupPath != "" {
			line += " backup=" + applied.BackupPath
		}
		fmt.Fprintln(stdout, line)
	}
	for _, manual := range result.Manual {
		fmt.Fprintf(stdout, "  manual %s: %s\n", manual.Path, manual.Note)
	}
}

func printRecovery(stdout io.Writer, result adapters.RecoveryResult) {
	if !result.Recovered {
		return
	}
	for _, path := range result.Restored {
		fmt.Fprintf(stdout, "recovered %s from backup\n", path)
	}
	for _, path := range result.Removed {
		fmt.Fprintf(stdout, "recovered %s (removed)\n", path)
	}
	for _, path := range result.Skipped {
		fmt.Fprintf(stdout, "recovery skipped %s: the file changed after the interrupted install\n", path)
	}
}

// printPiHints mirrors docs/08-pi-plugin.md section 7.3 when pi has no native
// plugin. It never runs `pi install` or writes settings because install --status
// must stay read-only.
func printPiHints(stdout io.Writer, plan adapters.InstallPlan) {
	if plan.Agent != "pi" || plan.NativePluginDetected || plan.Status != adapters.StatusNeedsAttention {
		return
	}
	version, err := piplugin.Version()
	if err != nil {
		return
	}
	fmt.Fprintf(stdout, "  npm:   pi install npm:memctl-pi@%s\n", version)
	fmt.Fprintln(stdout, "  离线:  memctl pi plugin-dir")
	fmt.Fprintln(stdout, "         pi install <输出的绝对目录>")
	fmt.Fprintln(stdout, "  校验:  pi list")
}

// printDshHints mirrors docs/07-dsh-plugin.md section 6.4 when dsh has no
// detected plugin. It never runs memctl dsh plugin-path itself because install
// --status must stay read-only.
func printDshHints(stdout io.Writer, plan adapters.InstallPlan) {
	if plan.Agent != "dsh" || plan.PluginVersion != "" || plan.Status != adapters.StatusNeedsAttention {
		return
	}
	version, err := dshplugin.Version()
	if err != nil {
		return
	}
	fmt.Fprintf(stdout, "  DSH 界面: 插件 → 添加插件 → memctl-dsh@%s\n", version)
	fmt.Fprintln(stdout, "  DSH 离线: 运行 memctl dsh plugin-path，把输出的绝对 tarball 路径粘贴到同一对话框")
	fmt.Fprintf(stdout, "  CLI: dsh plugin --profile <profile> add memctl-dsh@%s\n", version)
}
