package cli

import (
	"context"

	"github.com/benchkram/bob/pkg/boblog"
	"github.com/benchkram/bob/pkg/usererror"
	"github.com/benchkram/bob/tui"
	"github.com/pkg/errors"

	"github.com/benchkram/bob/bob"
	"github.com/benchkram/bob/bob/global"
	"github.com/benchkram/errz"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run interactive tasks",
	Args:  cobra.MinimumNArgs(0),
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		taskname := global.DefaultBuildTask
		if len(args) > 0 {
			taskname = args[0]
		}

		noCache, err := cmd.Flags().GetBool("no-cache")
		errz.Fatal(err)

		run(taskname, noCache)
	},
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		tasks, err := getRunTasks()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		return tasks, cobra.ShellCompDirectiveDefault
	},
}

func run(taskname string, noCache bool) {
	var err error
	defer errz.Recover(&err)

	b, err := bob.Bob(
		bob.WithCachingEnabled(!noCache),
	)
	errz.Fatal(err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t, err := tui.New()
	if err != nil {
		errz.Log(err)

		return
	}
	defer t.Restore()

	commander, err := b.Run(ctx, taskname)
	if err != nil {
		switch err {
		case bob.ErrNoRebuildRequired:
		default:
			if errors.As(err, &usererror.Err) {
				boblog.Log.UserError(err)
				return
			} else {
				errz.Fatal(err)
			}
		}
	}

	if commander != nil {
		t.Start(commander)
	}

	cancel()

	if commander != nil {
		<-commander.Done()
	}
}

func getRunTasks() ([]string, error) {
	b, err := bob.Bob()
	if err != nil {
		return nil, err
	}
	return b.GetRunTasks()
}
