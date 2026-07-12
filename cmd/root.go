// Package cmd wires the OrionDrive command-line interface.
package cmd

import (
	"fmt"
	"os"

	"github.com/NhProGamer/orion-drive/conf"
	"github.com/spf13/cobra"
)

var confPath string

// cfg is populated in PersistentPreRunE and shared by subcommands.
var cfg *conf.Config

var rootCmd = &cobra.Command{
	Use:           "orion-drive",
	Short:         "OrionDrive — self-hosted file management platform",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		c, err := conf.Load(confPath)
		if err != nil {
			return err
		}
		cfg = c
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&confPath, "config", "c", "conf.ini", "path to the configuration file")
}

// Execute runs the root command and exits non-zero on error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
