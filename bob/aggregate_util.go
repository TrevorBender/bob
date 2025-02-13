package bob

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/benchkram/bob/bob/bobfile"
	"github.com/benchkram/bob/pkg/usererror"
	"github.com/benchkram/errz"
)

// syncProjectName project names for all bobfiles and build tasks
func syncProjectName(
	a *bobfile.Bobfile,
	bobs []*bobfile.Bobfile,
) (*bobfile.Bobfile, []*bobfile.Bobfile) {
	for _, bobfile := range bobs {
		bobfile.Project = a.Project

		for taskname, task := range bobfile.BTasks {
			// Should be the name of the umbrella-bobfile.
			task.SetProject(a.Project)

			// Overwrite value in build map
			bobfile.BTasks[taskname] = task
		}
	}

	return a, bobs
}

func (b *B) addBuildTasksToAggregate(
	a *bobfile.Bobfile,
	bobs []*bobfile.Bobfile,
) *bobfile.Bobfile {

	for _, bobfile := range bobs {
		// Skip the aggregate
		if bobfile.Dir() == a.Dir() {
			continue
		}

		for taskname, task := range bobfile.BTasks {
			dir := bobfile.Dir()

			// Use a relative path as task prefix.
			prefix := strings.TrimPrefix(dir, b.dir)
			taskname := addTaskPrefix(prefix, taskname)

			// Alter the taskname.
			task.SetName(taskname)

			// Rewrite dependent tasks to global scope.
			dependsOn := []string{}
			for _, dependentTask := range task.DependsOn {
				dependsOn = append(dependsOn, addTaskPrefix(prefix, dependentTask))
			}
			task.DependsOn = dependsOn

			a.BTasks[taskname] = task
		}
	}

	return a
}

func (b *B) addRunTasksToAggregate(
	a *bobfile.Bobfile,
	bobs []*bobfile.Bobfile,
) *bobfile.Bobfile {

	for _, bobfile := range bobs {
		// Skip the aggregate
		if bobfile.Dir() == a.Dir() {
			continue
		}

		for runname, run := range bobfile.RTasks {
			dir := bobfile.Dir()

			// Use a relative path as task prefix.
			prefix := strings.TrimPrefix(dir, b.dir)

			runname = addTaskPrefix(prefix, runname)

			// Alter the runname.
			run.SetName(runname)

			// Rewrite dependents to global scope.
			dependsOn := []string{}
			for _, dependent := range run.DependsOn {
				dependsOn = append(dependsOn, addTaskPrefix(prefix, dependent))
			}
			run.DependsOn = dependsOn

			a.RTasks[runname] = run
		}
	}

	return a
}

// readImports recursively
//
// readModePlain allows to read bobfiles without
// doing sanitization.
//
// If prefix is given it's appended to the search path to asuure
// correctness of the search path in case of recursive calls.
func readImports(
	a *bobfile.Bobfile,
	readModePlain bool,
	prefix ...string,
) (imports []*bobfile.Bobfile, err error) {
	errz.Recover(&err)

	var p string
	if len(prefix) > 0 {
		p = prefix[0]
	}

	imports = []*bobfile.Bobfile{}
	for _, imp := range a.Imports {
		// read bobfile
		var boblet *bobfile.Bobfile
		var err error
		if readModePlain {
			boblet, err = bobfile.BobfileReadPlain(filepath.Join(p, imp))
		} else {
			boblet, err = bobfile.BobfileRead(filepath.Join(p, imp))
		}
		if err != nil {
			if errors.Is(err, bobfile.ErrBobfileNotFound) {
				return nil, usererror.Wrap(err)
			}
			errz.Fatal(err)
		}
		imports = append(imports, boblet)

		// read imports rescursively
		childImports, err := readImports(boblet, readModePlain, boblet.Dir())
		errz.Fatal(err)
		imports = append(imports, childImports...)
	}

	return imports, nil
}
