//go:build mage

// Tools for building and maintaining the Slims implementation of Orv.
package main

import (
	"errors"
	"os"
	"os/exec"

	"github.com/magefile/mage/sh"
)

// Recompiles protobuf contracts.
func CompilePB() error {
	// ensure protoc-gen-go and protoc are available
	if _, err := exec.LookPath("protoc-gen-go"); err != nil {
		return errors.Join(errors.New("protoc-gen-go not available in $PATH"), err)
	} else if _, err := exec.LookPath("protoc"); err != nil {
		return errors.Join(errors.New("protoc not available in $PATH"), err)
	}
	sh.Exec(nil, os.Stdout, os.Stderr, "protoc", "--go_out=.", "--go_opt=paths=source_relative", "slims/pb/payloads.proto")
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
	_, err := sh.Exec(nil, os.Stdout, os.Stderr, "go", "test", "./...", "-race", "-count=1")
	return err
}
