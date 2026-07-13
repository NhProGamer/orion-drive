package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/spf13/cobra"
)

var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Manage storage policies",
}

// repoForCLI opens the database, applies migrations and returns a repository.
func repoForCLI() (*repository.Repository, error) {
	db, err := bootstrap.OpenDatabase(cfg)
	if err != nil {
		return nil, err
	}
	if err := bootstrap.Migrate(db, cfg); err != nil {
		return nil, err
	}
	return repository.New(db), nil
}

var policyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List storage policies",
	RunE: func(*cobra.Command, []string) error {
		repo, err := repoForCLI()
		if err != nil {
			return err
		}
		policies, err := repo.Policy.List(context.Background())
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tBUCKET\tENDPOINT\tBASE_PATH")
		for _, p := range policies {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Type, p.BucketName, p.Server, p.BasePath)
		}
		return w.Flush()
	},
}

var (
	s3Name      string
	s3Bucket    string
	s3Endpoint  string
	s3Region    string
	s3AccessKey string
	s3SecretKey string
	s3BasePath  string
	s3PathStyle bool
)

var policyAddS3Cmd = &cobra.Command{
	Use:   "add-s3",
	Short: "Create an S3 (or S3-compatible) storage policy",
	RunE: func(*cobra.Command, []string) error {
		if s3Bucket == "" {
			return fmt.Errorf("--bucket is required")
		}
		repo, err := repoForCLI()
		if err != nil {
			return err
		}
		settings, _ := json.Marshal(map[string]any{"region": s3Region, "path_style": s3PathStyle})
		p := &model.StoragePolicy{
			Name:       s3Name,
			Type:       model.PolicyTypeS3,
			Server:     s3Endpoint,
			BucketName: s3Bucket,
			BasePath:   s3BasePath,
			AccessKey:  s3AccessKey,
			SecretKey:  s3SecretKey,
			Settings:   model.JSON(settings),
		}
		if err := repo.Policy.Create(context.Background(), p); err != nil {
			return err
		}
		fmt.Printf("created S3 policy #%d (%s)\n", p.ID, p.Name)
		return nil
	},
}

var (
	localName     string
	localBasePath string
	localEncrypt  bool
)

var policyAddLocalCmd = &cobra.Command{
	Use:   "add-local",
	Short: "Create a local-disk storage policy",
	RunE: func(*cobra.Command, []string) error {
		repo, err := repoForCLI()
		if err != nil {
			return err
		}
		var settings model.JSON
		if localEncrypt {
			raw, _ := json.Marshal(map[string]any{"encrypt": true})
			settings = model.JSON(raw)
		}
		p := &model.StoragePolicy{
			Name:     localName,
			Type:     model.PolicyTypeLocal,
			BasePath: localBasePath,
			Settings: settings,
		}
		if err := repo.Policy.Create(context.Background(), p); err != nil {
			return err
		}
		fmt.Printf("created local policy #%d (%s, encrypt=%v)\n", p.ID, p.Name, localEncrypt)
		return nil
	},
}

var (
	remoteName     string
	remoteServer   string
	remoteSecret   string
	remoteBasePath string
)

var policyAddRemoteCmd = &cobra.Command{
	Use:   "add-remote",
	Short: "Create a remote (slave node) storage policy",
	RunE: func(*cobra.Command, []string) error {
		if remoteServer == "" {
			return fmt.Errorf("--server is required")
		}
		if remoteSecret == "" {
			return fmt.Errorf("--secret is required")
		}
		repo, err := repoForCLI()
		if err != nil {
			return err
		}
		p := &model.StoragePolicy{
			Name:      remoteName,
			Type:      model.PolicyTypeRemote,
			Server:    remoteServer,
			BasePath:  remoteBasePath,
			SecretKey: remoteSecret,
		}
		if err := repo.Policy.Create(context.Background(), p); err != nil {
			return err
		}
		fmt.Printf("created remote policy #%d (%s)\n", p.ID, p.Name)
		return nil
	},
}

var (
	assignGroup  uint
	assignPolicy uint
)

var policyAssignCmd = &cobra.Command{
	Use:   "assign",
	Short: "Assign a storage policy to a group",
	RunE: func(*cobra.Command, []string) error {
		repo, err := repoForCLI()
		if err != nil {
			return err
		}
		if _, err := repo.Policy.GetByID(context.Background(), assignPolicy); err != nil {
			return fmt.Errorf("policy %d: %w", assignPolicy, err)
		}
		if err := repo.Group.SetStoragePolicy(context.Background(), assignGroup, assignPolicy); err != nil {
			return err
		}
		fmt.Printf("group %d now uses policy %d\n", assignGroup, assignPolicy)
		return nil
	},
}

func init() {
	policyAddS3Cmd.Flags().StringVar(&s3Name, "name", "S3", "policy name")
	policyAddS3Cmd.Flags().StringVar(&s3Bucket, "bucket", "", "bucket name (required)")
	policyAddS3Cmd.Flags().StringVar(&s3Endpoint, "endpoint", "", "custom endpoint URL for S3-compatible services")
	policyAddS3Cmd.Flags().StringVar(&s3Region, "region", "us-east-1", "region")
	policyAddS3Cmd.Flags().StringVar(&s3AccessKey, "access-key", "", "access key")
	policyAddS3Cmd.Flags().StringVar(&s3SecretKey, "secret-key", "", "secret key")
	policyAddS3Cmd.Flags().StringVar(&s3BasePath, "base-path", "", "key prefix within the bucket")
	policyAddS3Cmd.Flags().BoolVar(&s3PathStyle, "path-style", false, "use path-style addressing (required by most S3-compatible servers)")

	policyAddLocalCmd.Flags().StringVar(&localName, "name", "Local", "policy name")
	policyAddLocalCmd.Flags().StringVar(&localBasePath, "base-path", "data/storage", "base directory on disk")
	policyAddLocalCmd.Flags().BoolVar(&localEncrypt, "encrypt", false, "encrypt stored objects at rest (requires [Storage] EncryptionKey)")

	policyAddRemoteCmd.Flags().StringVar(&remoteName, "name", "Remote", "policy name")
	policyAddRemoteCmd.Flags().StringVar(&remoteServer, "server", "", "slave node base URL, e.g. http://slave:5212 (required)")
	policyAddRemoteCmd.Flags().StringVar(&remoteSecret, "secret", "", "shared signing secret matching the slave (required)")
	policyAddRemoteCmd.Flags().StringVar(&remoteBasePath, "base-path", "", "key prefix within the slave storage")

	policyAssignCmd.Flags().UintVar(&assignGroup, "group", 1, "group ID")
	policyAssignCmd.Flags().UintVar(&assignPolicy, "policy", 0, "policy ID")
	_ = policyAssignCmd.MarkFlagRequired("policy")

	policyCmd.AddCommand(policyListCmd, policyAddLocalCmd, policyAddS3Cmd, policyAddRemoteCmd, policyAssignCmd)
	rootCmd.AddCommand(policyCmd)
}
