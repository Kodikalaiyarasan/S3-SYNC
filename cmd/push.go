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
	pushConcurrency int
	pushDryRun      bool
	pushDelete      bool
)

var pushCmd = &cobra.Command{
	Use:   "push <local-dir> s3://<bucket-name>/<prefix>",
	Short: "Upload and mirror local directory to S3",
	Long:  `Recursively scans the local directory and uploads new/modified files to the S3 bucket and prefix.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		localDir := args[0]
		s3URI := args[1]

		bucket, prefix, err := s3client.ParseS3URI(s3URI)
		if err != nil {
			return err
		}

		ctx := context.Background()
		client, err := GetS3Client(ctx)
		if err != nil {
			return fmt.Errorf("failed to initialize S3 client: %w", err)
		}

		engine := sync.NewEngine(client, pushConcurrency)

		fmt.Printf("Starting Push: %s -> s3://%s/%s (Workers: %d)\n", localDir, bucket, prefix, pushConcurrency)
		startTime := time.Now()

		summary, err := engine.Push(ctx, sync.PushOptions{
			Bucket:   bucket,
			Prefix:   prefix,
			LocalDir: localDir,
			DryRun:   pushDryRun,
			Delete:   pushDelete,
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
	rootCmd.AddCommand(pushCmd)

	pushCmd.Flags().IntVarP(&pushConcurrency, "concurrency", "c", 10, "Number of parallel upload workers")
	pushCmd.Flags().BoolVar(&pushDryRun, "dry-run", false, "Preview files that would be uploaded without transferring data")
	pushCmd.Flags().BoolVar(&pushDelete, "delete", false, "Remove objects from S3 if they no longer exist in the local directory")
}
