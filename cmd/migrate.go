package cmd

import (
	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate [up|down|status|version|reset|redo]",
	Short: "Manage the database schema with goose",
	Long: "Run database migrations. With no argument it applies all pending " +
		"migrations (up). Other goose commands are forwarded: down, status, " +
		"version, reset, redo, up-by-one.",
	Args: cobra.ArbitraryArgs,
	RunE: func(_ *cobra.Command, args []string) error {
		db, err := bootstrap.OpenDatabase(cfg)
		if err != nil {
			return err
		}
		command := "up"
		var rest []string
		if len(args) > 0 {
			command = args[0]
			rest = args[1:]
		}
		return bootstrap.RunGoose(db, cfg, command, rest...)
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}
