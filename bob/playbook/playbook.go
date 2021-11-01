package playbook

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Benchkram/bob/bobtask"
	"github.com/Benchkram/bob/bobtask/hash"
	"github.com/Benchkram/bob/pkg/boblog"
	"github.com/Benchkram/errz"
)

// The playbook defines the order in which tasks are allowed to run.
// Also determines the possibility to run tasks in parallel.

var ErrTaskDoesNotExist = fmt.Errorf("task does not exist")
var ErrDone = fmt.Errorf("playbook is done")
var ErrFailed = fmt.Errorf("playbook failed")
var ErrUnexpectedTaskState = fmt.Errorf("task state is unsexpected")

type Playbook struct {
	// taskChannel is closed when the root
	// task completes.
	taskChannel chan bobtask.Task

	// errorChannel to transport errors to the caller
	errorChannel chan error

	// root task
	root string

	Tasks StatusMap

	done bool
}

func New(root string) *Playbook {
	p := &Playbook{
		taskChannel:  make(chan bobtask.Task, 10),
		errorChannel: make(chan error),
		Tasks:        make(StatusMap),
		root:         root,
	}
	return p
}

// TaskNeedsRebuild check if a tasks need a rebuild by looking at it's hash value
// and it's child tasks.
func (p *Playbook) TaskNeedsRebuild(taskname string) (bool, error) {
	ts, ok := p.Tasks[taskname]
	if !ok {
		return true, ErrTaskDoesNotExist
	}
	task := ts.Task

	hash, err := task.Hash()
	if err != nil {
		return true, err
	}

	// Check if task itself needs a rebuild
	rebuildRequired, err := task.NeedsRebuild(&bobtask.RebuildOptions{Hash: hash})
	if err != nil {
		if rebuildRequired {
			boblog.Log.V(3).Info(fmt.Sprintf("[task:%s] rebuild required: input changed", taskname))
		}
		return rebuildRequired, err
	}

	var Done = fmt.Errorf("done")
	// Check if task needs a rebuild due to its dependencies changing
	err = p.Tasks.walk(task.Name(), func(tn string, t *Status, err error) error {
		if err != nil {
			return err
		}

		// Ignore the task itself
		if task.Name() == tn {
			return nil
		}

		// Require a rebuild if the dependend task did require a rebuild
		if t.State() != StateNoRebuildRequired {
			boblog.Log.V(3).Info(fmt.Sprintf("[task:%s] rebuild required: dependecy changed", taskname))
			rebuildRequired = true
			// Bail out early
			return Done
		}

		return nil
	})

	if !rebuildRequired {
		target, err := task.Target()
		if err != nil {
			return true, err
		}

		if target != nil {
			// On a invalid traget a rebuild is required
			rebuildRequired = !target.Verify()
			if rebuildRequired {
				boblog.Log.V(3).Info(fmt.Sprintf("[task:%s] rebuild required: targets are invalid", taskname))
			}
		}

		// TODO: check for validity of targets
		// and also sync with a remote artifact provider
		//
		// As a starting point its enough to check validity
		// based on the targets hash stored in the filename
		// (see .bobcache)
	}

	if errors.Is(err, Done) {
		return rebuildRequired, nil
	}

	return rebuildRequired, err
}

func (p *Playbook) Play() (err error) {
	return p.play()
}

func (p *Playbook) play() error {

	if p.done {
		return ErrDone
	}

	playbookDone := func() {
		close(p.taskChannel)
		p.done = true
	}

	// Walk the task chain and determine the next build task. Send it to the task channel.
	// Returns `taskQueued` when a task has been send to the taskChannel.
	// Returns `taskFailed` when a task has failed.
	// Once it returns `nil` the playbook is usually done with it's work.
	var taskQueued = fmt.Errorf("task queued")
	var taskFailed = fmt.Errorf("task failed")
	err := p.Tasks.walk(p.root, func(taskname string, task *Status, err error) error {
		if err != nil {
			return err
		}

		// fmt.Printf("walking task %s which is in state %s\n", taskname, task.State())

		switch task.State() {
		case StatePending:
			// Check if all dependent tasks are completed
			for _, dependentTaskName := range task.Task.DependsOn {
				t, ok := p.Tasks[dependentTaskName]
				if !ok {
					//fmt.Printf("Task %s does not exist", dependentTaskName)
					return ErrTaskDoesNotExist
				}
				// fmt.Printf("dependentTask %s which is in state %s\n", t.Task.Name(), t.State())

				state := t.State()
				if state != StateCompleted && state != StateNoRebuildRequired {
					// A dependent task is not completed.
					// So this task is not yet ready to run.
					return nil
				}
			}
		case StateFailed:
			return taskFailed
		case StateNoRebuildRequired:
			return nil
		case StateCompleted:
			return nil
		default:
		}

		// fmt.Printf("sending task %s to channel\n", task.Task.Name())
		p.taskChannel <- task.Task
		return taskQueued
	})

	// taskQueued => return nil (happy path)
	// taskFailed => return PlaybookFailed
	// default    => return err
	if err != nil {
		if errors.Is(err, taskQueued) {
			return nil
		}
		if errors.Is(err, taskFailed) {
			return ErrFailed
		}
		return err
	}

	// no work done, usually happens when
	// no task needs a rebuild.
	playbookDone()

	return nil
}

// TaskChannel returns the next task
func (p *Playbook) TaskChannel() <-chan bobtask.Task {
	return p.taskChannel
}

func (p *Playbook) ErrorChannel() <-chan error {
	return p.errorChannel
}

func (p *Playbook) setTaskState(taskname, state string) error {
	task, ok := p.Tasks[taskname]
	if !ok {
		return ErrTaskDoesNotExist
	}

	task.SetState(state)
	switch state {
	case StateCompleted, StateCanceled, StateNoRebuildRequired:
		task.End = time.Now()
	}

	p.Tasks[taskname] = task
	return nil
}

func (p *Playbook) pack(taskname string, hash string) error {
	task, ok := p.Tasks[taskname]
	if !ok {
		return ErrTaskDoesNotExist
	}
	return task.Task.Pack(hash)
}

func (p *Playbook) storeHash(taskname string, hash *hash.Task) error {
	task, ok := p.Tasks[taskname]
	if !ok {
		return ErrTaskDoesNotExist
	}

	return task.Task.StoreHash(hash)
}

func (p *Playbook) ExecutionTime() time.Duration {
	var start, end *Status

	for _, task := range p.Tasks {
		if start == nil || start.Start.After(task.Start) {
			start = task
		}
		if end == nil || end.End.Before(task.End) {
			end = task
		}
	}

	return end.End.Sub(start.Start)
}

// TaskStatus returns the current state of a task
func (p *Playbook) TaskStatus(taskname string) (ts *Status, _ error) {
	status, ok := p.Tasks[taskname]
	if !ok {
		return ts, ErrTaskDoesNotExist
	}
	return status, nil
}

// TaskCompleted sets a task to completed
func (p *Playbook) TaskCompleted(taskname string) (err error) {
	defer errz.Recover(&err)

	task, ok := p.Tasks[taskname]
	if !ok {
		return ErrTaskDoesNotExist
	}

	// compute input hash & target hash
	hash, err := task.Task.Hash()
	if err != nil {
		return err
	}

	target, err := task.Task.Target()
	if err != nil {
		return err
	}

	if target != nil {
		targetHash, err := target.Hash()
		if err != nil {
			return err
		}
		hash.Targets[taskname] = targetHash

		// gather target hashes of dependent tasks
		err = p.Tasks.walk(taskname, func(tn string, task *Status, err error) error {
			if err != nil {
				return err
			}
			if taskname == tn {
				return nil
			}

			target, err := task.Target()
			if err != nil {
				return err
			}
			if target == nil {
				return nil
			}

			switch task.State() {
			case StateCompleted:
				fallthrough
			case StateNoRebuildRequired:
				h, err := target.Hash()
				if err != nil {
					return err
				}
				hash.Targets[task.Task.Name()] = h
			default:
				return ErrUnexpectedTaskState
			}

			return nil
		})
		errz.Fatal(err)
	}

	err = p.storeHash(taskname, hash)
	errz.Fatal(err)

	// TODO: use target hash?
	err = p.pack(taskname, hash.Input)
	errz.Fatal(err)

	err = p.setTaskState(taskname, StateCompleted)
	errz.Fatal(err)

	err = p.play()
	if err != nil {
		if !errors.Is(err, ErrDone) {
			errz.Fatal(err)
		}
	}

	return nil
}

// TaskNoRebuildRequired sets a task's state to indicate that no rebuild is required
func (p *Playbook) TaskNoRebuildRequired(taskname string) (err error) {
	defer errz.Recover(&err)

	err = p.setTaskState(taskname, StateNoRebuildRequired)
	errz.Fatal(err)

	err = p.play()
	if err != nil {
		if !errors.Is(err, ErrDone) {
			errz.Fatal(err)
		}
	}

	return nil
}

// TaskFailed sets a task to failed
func (p *Playbook) TaskFailed(taskname string) (err error) {
	defer errz.Recover(&err)

	err = p.setTaskState(taskname, StateFailed)
	errz.Fatal(err)

	// p.errorChannel <- fmt.Errorf("Task %s failed", taskname)

	// give the playbook the chance to set
	// the state to done.
	err = p.play()
	if err != nil {
		if !errors.Is(err, ErrDone) {
			errz.Fatal(err)
		}
	}

	return nil
}

// TaskCanceled sets a task to canceled
func (p *Playbook) TaskCanceled(taskname string) (err error) {
	defer errz.Recover(&err)

	err = p.setTaskState(taskname, StateCanceled)
	errz.Fatal(err)

	// p.errorChannel <- fmt.Errorf("Task %s cancelled", taskname)

	return nil
}

func (p *Playbook) List() (err error) {
	defer errz.Recover(&err)

	keys := make([]string, 0, len(p.Tasks))
	for k := range p.Tasks {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Println(k)
	}

	return nil
}

func (p *Playbook) String() string {
	description := bytes.NewBufferString("")

	fmt.Fprint(description, "Playbook:\n")

	keys := make([]string, 0, len(p.Tasks))
	for k := range p.Tasks {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		task := p.Tasks[k]
		fmt.Fprintf(description, "  %s(%s): %s\n", k, task.Task.Name(), task.State())
	}

	return description.String()
}

const (
	StatePending           string = "pending"
	StateCompleted         string = "completed-built"
	StateNoRebuildRequired string = "no-rebuild-required"
	StateFailed            string = "failed"
	StateCanceled          string = "canceled"
)
