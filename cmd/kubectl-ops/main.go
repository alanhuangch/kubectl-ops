package main

import (
	"os"

	"github.com/alanhuangch/kubectl-ops/internal/command"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func main() {
	streams := genericclioptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	if err := command.NewRootCommand(streams).Execute(); err != nil {
		os.Exit(1)
	}
}
