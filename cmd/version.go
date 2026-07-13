package cmd

import (
	"fmt"

	"github.com/NhProGamer/orion-drive/application/constants"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:               "version",
	Short:             "Print the OrionDrive version",
	PersistentPreRunE: func(*cobra.Command, []string) error { return nil }, // no config needed
	Run: func(*cobra.Command, []string) {
		fmt.Printf("OrionDrive %s (commit %s, built %s)\n", constants.Version, constants.Commit, constants.Date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
