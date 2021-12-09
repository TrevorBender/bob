package bobfile

import (
	"bytes"
	"fmt"
	"github.com/Benchkram/bob/pkg/usererror"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-version"
	"github.com/pkg/errors"

	"gopkg.in/yaml.v3"

	"github.com/Benchkram/errz"

	"github.com/Benchkram/bob/bob/global"
	"github.com/Benchkram/bob/bobrun"
	"github.com/Benchkram/bob/bobtask"
	"github.com/Benchkram/bob/pkg/file"
)

var (
	defaultIgnores = fmt.Sprintf("!%s\n!%s",
		global.BobWorkspaceFile,
		filepath.Join(global.BobCacheDir, "*"),
	)
)

var (
	ErrNotImplemented         = fmt.Errorf("Not implemented")
	ErrBobfileNotFound        = fmt.Errorf("Could not find a Bobfile")
	ErrHashesFileDoesNotExist = fmt.Errorf("Hashes file does not exist")
	ErrTaskHashDoesNotExist   = fmt.Errorf("Task hash does not exist")
	ErrBobfileExists          = fmt.Errorf("Bobfile exists")
	ErrTaskDoesNotExist       = fmt.Errorf("Task does not exist")
	ErrDuplicateTaskName      = fmt.Errorf("duplicate task name")
	ErrSelfReference          = fmt.Errorf("self reference")

	ErrInvalidRunType = fmt.Errorf("Invalid run type")
)

type Bobfile struct {
	Version string `yaml:"version,omitempty"`

	Variables VariableMap

	Tasks bobtask.Map

	Runs bobrun.RunMap

	// Parent directory of the Bobfile.
	// Populated through BobfileRead().
	dir string

	bobfiles []*Bobfile
}

func NewBobfile() *Bobfile {
	b := &Bobfile{
		Variables: make(VariableMap),
		Tasks:     make(bobtask.Map),
		Runs:      make(bobrun.RunMap),
	}
	return b
}

func (b *Bobfile) SetBobfiles(bobs []*Bobfile) {
	b.bobfiles = bobs
}

func (b *Bobfile) Bobfiles() []*Bobfile {
	return b.bobfiles
}

// bobfileRead reads a bobfile and intializes private fields.
func bobfileRead(dir string) (_ *Bobfile, err error) {
	defer errz.Recover(&err)

	bobfilePath := filepath.Join(dir, global.BobFileName)

	if !file.Exists(bobfilePath) {
		return nil, ErrBobfileNotFound
	}
	bin, err := ioutil.ReadFile(bobfilePath)
	errz.Fatal(err, "Failed to read config file")

	bobfile := &Bobfile{
		dir: dir,
	}

	err = yaml.Unmarshal(bin, bobfile)
	if err != nil {
		return nil, usererror.Wrapm(err, "YAML unmarshal failed")
	}

	if bobfile.Variables == nil {
		bobfile.Variables = VariableMap{}
	}

	if bobfile.Tasks == nil {
		bobfile.Tasks = bobtask.Map{}
	}

	if bobfile.Runs == nil {
		bobfile.Runs = bobrun.RunMap{}
	}

	// Assure tasks are initialized with their defaults
	for key, task := range bobfile.Tasks {
		task.SetDir(bobfile.dir)
		task.SetName(key)

		task.InputDirty = fmt.Sprintf("%s\n%s", task.InputDirty, defaultIgnores)

		// Make sure a task is correctly initialised.
		// TODO: All unitialised must be initialised or get default values.
		// This means switching to pointer types for most members.
		task.SetEnv([]string{})

		bobfile.Tasks[key] = task
	}

	// Assure runs are initialized with their defaults
	for key, run := range bobfile.Runs {
		run.SetDir(bobfile.dir)
		run.SetName(key)

		bobfile.Runs[key] = run
	}

	return bobfile, nil
}

// BobfileRead read from a bobfile.
// Calls sanitize on the result.
func BobfileRead(dir string) (_ *Bobfile, err error) {
	defer errz.Recover(&err)

	b, err := bobfileRead(dir)
	errz.Fatal(err)

	err = b.Validate()
	errz.Fatal(err)

	b.Tasks.Sanitize()

	return b, nil
}

// Validate makes sure no task depends on itself (self-reference) or has the same name as another task
func (b *Bobfile) Validate() (err error) {
	if b.Version != "" {
		_, err = version.NewVersion(b.Version)
		if err != nil {
			return fmt.Errorf("invalid version '%s' (%s)", b.Version, b.Dir())
		}
	}

	// use for duplicate names validation
	names := map[string]bool{}

	for name, task := range b.Tasks {
		// validate no duplicate name
		if names[name] {
			return errors.WithMessage(ErrDuplicateTaskName, name)
		}

		names[name] = true

		// validate no self-reference
		for _, dep := range task.DependsOn {
			if name == dep {
				return errors.WithMessage(ErrSelfReference, name)
			}
		}
	}

	for name, run := range b.Runs {
		// validate no duplicate name
		if names[name] {
			return errors.WithMessage(ErrDuplicateTaskName, name)
		}

		names[name] = true

		// validate no self-reference
		for _, dep := range run.DependsOn {
			if name == dep {
				return errors.WithMessage(ErrSelfReference, name)
			}
		}
	}

	return nil
}

func (b *Bobfile) BobfileSave(dir string) (err error) {
	defer errz.Recover(&err)

	buf := bytes.NewBuffer([]byte{})

	encoder := yaml.NewEncoder(buf)
	encoder.SetIndent(2)
	defer encoder.Close()

	err = encoder.Encode(b)
	errz.Fatal(err)

	return ioutil.WriteFile(filepath.Join(dir, global.BobFileName), buf.Bytes(), 0664)
}

func (b *Bobfile) Dir() string {
	return b.dir
}

func CreateDummyBobfile(dir string, overwrite bool) (err error) {
	// Prevent accidential bobfile override
	if file.Exists(global.BobFileName) && !overwrite {
		return ErrBobfileExists
	}

	bobfile := NewBobfile()

	bobfile.Tasks[global.DefaultBuildTask] = bobtask.Task{
		InputDirty:  "./main.go",
		CmdDirty:    "go build -o run",
		TargetDirty: "run",
	}
	return bobfile.BobfileSave(dir)
}

func IsBobfile(file string) bool {
	return strings.Contains(filepath.Base(file), global.BobFileName)
}
