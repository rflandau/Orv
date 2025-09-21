//go:build mage

// Tools for building and maintaining the Slims implementation of Orv.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/magefile/mage/sh"
)

// Recompiles all protobuf contracts.
func CompilePB() error {
	// ensure protoc-gen-go and protoc are available
	if _, err := exec.LookPath("protoc-gen-go"); err != nil {
		return errors.Join(errors.New("protoc-gen-go not available in $PATH"), err)
	} else if _, err := exec.LookPath("protoc"); err != nil {
		return errors.Join(errors.New("protoc not available in $PATH"), err)
	}
	sh.Exec(nil, os.Stdout, os.Stderr, "protoc", "--go_out=.", "--go_opt=paths=source_relative", "slims/pb/payloads.proto")
	sh.Exec(nil, os.Stdout, os.Stderr, "protoc", "--go_out=.", "--go_opt=paths=source_relative", "slims/pb/message_types.proto")
	return nil
}

// Lint the protobuf files.
// If suppress is true, this command swallows EnumField prefix warnings.
func LintPB(suppress bool) error {
	if _, err := exec.LookPath("protolint"); err != nil {
		return errors.Join(errors.New("protolint not available in $PATH"), err)
	}

	sbOut, sbErr := strings.Builder{}, strings.Builder{}
	splitInput := func(sb *strings.Builder) {
		for line := range strings.Lines(sb.String()) {
			if matched, err := regexp.MatchString(`EnumField name "\w+" should have the prefix "\w+"`, line); err != nil {
				fmt.Println("---failed to regex match: %v---", err)
			} else if !suppress || !matched {
				fmt.Print(line)
			}
		}
	}

	// protolint spits out a non-zero exit code  if any errors occurred, so we need to print from the appropriate writer
	if ran, err := sh.Exec(nil, &sbOut, &sbErr, "protolint", "slims/pb/"); err != nil {
		splitInput(&sbErr)
		//return fmt.Errorf("%w: %v", err, sbErr.String())
	} else if !ran {
		return errors.New("failed to run")
	} else {
		splitInput(&sbOut)
	}

	return nil
}

// NYI
func Build() error {
	//mg.Deps(CompileProtoBufs)
	// TODO
	return nil
}

// Runs all Slims tests.
// Tests are run with -race.
func Test() error {
	_, err := sh.Exec(nil, os.Stdout, os.Stderr, "go", "test", "./...", "-race", "-count=1", "-cover")
	return err
}
