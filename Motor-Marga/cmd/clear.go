package cmd

import (
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

var clear = cobra.Command{
	Use:     "clear",
	Aliases: []string{"cls"},
	Short:   "This command clear all the text in the terminal",
	Run: func(cmd *cobra.Command, args []string) {
		CallClear()
	},
}

func CallClear() {
	var c *exec.Cmd

	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/c", "cls")
	} else {
		c = exec.Command("clear")
	}
	c.Stdout = os.Stdout
	_ = c.Run()
}

func init() {
	RootCmd.AddCommand(&clear)
}
