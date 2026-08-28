package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use: "marga",
}

func init() {
	RootCmd.AddCommand(&ls)
	RootCmd.AddCommand(cd)
	RootCmd.AddCommand(&mkdir)
	RootCmd.AddCommand(create)
	RootCmd.AddCommand(&clear)
}

func Execute() {
	err := RootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
