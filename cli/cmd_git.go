package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Benchkram/bob/bobgit"
	"github.com/Benchkram/bob/pkg/boblog"
	"github.com/Benchkram/bob/pkg/bobutil"
	"github.com/Benchkram/bob/pkg/usererror"
	"github.com/Benchkram/errz"
)

var CmdGit = &cobra.Command{
	Use:   "git",
	Short: "Run git cmds on all child repos",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		err := cmd.Help()
		errz.Fatal(err)
	},
}

var CmdGitStatus = &cobra.Command{
	Use:   "status",
	Short: "Run git status on all child repos",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		runGitStatus()
	},
}

var CmdGitAdd = &cobra.Command{
	Use:   "add",
	Short: "Run git add on all child repos",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		runGitAdd(args...)
	},
}

func runGitStatus() {
	s, err := bobgit.Status()
	if err != nil {
		if errors.Is(err, bobutil.ErrCouldNotFindBobWorkspace) {
			fmt.Println("fatal: not a bob repository (or any of the parent directories): .bob")
			os.Exit(1)
		}
		if errors.Is(err, bobgit.ErrCouldNotFindGitDir) {
			fmt.Println("fatal: bob workspace is not a git repository")
			os.Exit(1)
		}
		errz.Fatal(err)
	}
	fmt.Println(s.String())
}

func runGitAdd(targets ...string) {
	err := bobgit.Add(targets...)
	if err != nil {
		if errors.As(err, &usererror.Err) {
			boblog.Log.UserError(err)
			os.Exit(1)
		} else {
			errz.Fatal(err)
		}
	}
}
