package cmd

import (
	"fmt"

	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Create or update the database schema and seed defaults",
	RunE: func(*cobra.Command, []string) error {
		db, err := bootstrap.OpenDatabase(cfg)
		if err != nil {
			return err
		}
		if err := bootstrap.Migrate(db, cfg); err != nil {
			return err
		}
		fmt.Println("migration complete")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}
