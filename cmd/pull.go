package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"s3-sync/pkg/s3client"
	"s3-sync/pkg/sync"
	"s3-sync/pkg/ui"
)

var (
	pullConcurrency int
	pullDryRun      bool
	pullDelete      bool
)

var pullCmd = &cobra.Command{
	Use:   "pull s3://<bucket-name>/<prefix> <local-dir>",
	Short: "Download and mirror S3 to local directory",
	Long:  `Recursively scans remote S3 objects and downloads new/modified files to the local directory.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		s3URI := args[0]
		localDir := args[1]

		bucket, prefix, err := s3client.ParseS3URI(s3URI)
		if err != nil {
			return err
		}

		ctx := context.Background()
		client, err := GetS3Client(ctx)
		if err != nil {
			return fmt.Errorf("failed to initialize S3 client: %w", err)
		}

		engine := sync.NewEngine(client, pullConcurrency)

		fmt.Printf("Starting Pull: s3://%s/%s -> %s (Workers: %d)\n", bucket, prefix, localDir, pullConcurrency)
		startTime := time.Now()

		summary, err := engine.Pull(ctx, sync.PullOptions{
			Bucket:   bucket,
			Prefix:   prefix,
			LocalDir: localDir,
			DryRun:   pullDryRun,
			Delete:   pullDelete,
		})
		if err != nil {
			return err
		}

		duration := time.Since(startTime)
		fmt.Printf("\n=== Sync Summary ===\n")
		fmt.Printf("Transferred: %d files (%s)\n", summary.TransferredFiles, ui.FormatBytes(summary.TransferredBytes))
		fmt.Printf("Skipped (In-Sync): %d files\n", summary.SkippedFiles)
		fmt.Printf("Deleted: %d files\n", summary.DeletedFiles)
		fmt.Printf("Elapsed Time: %s\n", duration.Round(time.Millisecond))

		if len(summary.Errors) > 0 {
			fmt.Fprintf(os.Stderr, "\nEncountered %d errors during sync:\n", len(summary.Errors))
			for _, e := range summary.Errors {
				fmt.Fprintf(os.Stderr, " - %v\n", e)
			}
			return fmt.Errorf("sync completed with errors")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(pullCmd)

	pullCmd.Flags().IntVarP(&pullConcurrency, "concurrency", "c", 10, "Number of parallel download workers")
	pullCmd.Flags().BoolVar(&pullDryRun, "dry-run", false, "Preview files that would be downloaded without writing to disk")
	pullCmd.Flags().BoolVar(&pullDelete, "delete", false, "Remove local files if they do not exist in the remote S3 prefix")
}
