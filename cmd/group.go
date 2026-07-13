package cmd

import (
	"context"
	"fmt"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/spf13/cobra"
)

var groupCmd = &cobra.Command{
	Use:   "group",
	Short: "Manage groups",
}

var (
	permGroupID  uint
	permCanShare bool
)

var groupPermCmd = &cobra.Command{
	Use:   "perm",
	Short: "Set a group's permissions",
	RunE: func(*cobra.Command, []string) error {
		repo, err := repoForCLI()
		if err != nil {
			return err
		}
		g, err := repo.Group.GetByID(context.Background(), permGroupID)
		if err != nil {
			return fmt.Errorf("group %d: %w", permGroupID, err)
		}
		perms := g.Perms()
		perms.Share = &permCanShare
		if err := repo.Group.SetPermissions(context.Background(), permGroupID, model.MustJSON(perms)); err != nil {
			return err
		}
		fmt.Printf("group %d: can_share=%v\n", permGroupID, permCanShare)
		return nil
	},
}

func init() {
	groupPermCmd.Flags().UintVar(&permGroupID, "group", 1, "group ID")
	groupPermCmd.Flags().BoolVar(&permCanShare, "share", true, "allow members to create share links")
	groupCmd.AddCommand(groupPermCmd)
	rootCmd.AddCommand(groupCmd)
}
