package cmd

import (
	"context"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"s3-sync/pkg/s3client"
	"s3-sync/pkg/sync"
	"s3-sync/pkg/ui"
)

var diffCmd = &cobra.Command{
	Use:   "diff <local-dir> s3://<bucket-name>/<prefix>",
	Short: "Hash comparison and audit between local and S3",
	Long:  `Performs delta calculation between local directory and remote S3 prefix and displays an ASCII audit table.`,
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

		engine := sync.NewEngine(client, 10)

		localManifest, err := sync.ScanLocalDir(localDir)
		if err != nil {
			return fmt.Errorf("scanning local directory failed: %w", err)
		}

		remoteManifest, err := engine.ListRemoteObjects(ctx, bucket, prefix)
		if err != nil {
			return fmt.Errorf("scanning S3 objects failed: %w", err)
		}

		diffs := sync.EvaluateDiff(localManifest, remoteManifest)

		// Sort diffs alphabetically by relative path
		sort.Slice(diffs, func(i, j int) bool {
			return diffs[i].RelPath < diffs[j].RelPath
		})

		headers := []string{"FILE PATH", "LOCAL SIZE", "REMOTE SIZE", "STATUS", "DETAILS"}
		var rows [][]string

		counts := map[sync.DiffStatus]int{
			sync.StatusInSync:      0,
			sync.StatusMissingInS3: 0,
			sync.StatusModified:    0,
			sync.StatusOrphanInS3:  0,
		}

		for _, item := range diffs {
			counts[item.Status]++
			localSizeStr := "-"
			if item.LocalSize > 0 || item.Status != sync.StatusOrphanInS3 {
				localSizeStr = fmt.Sprintf("%d B", item.LocalSize)
			}
			remoteSizeStr := "-"
			if item.RemoteSize > 0 || item.Status != sync.StatusMissingInS3 {
				remoteSizeStr = fmt.Sprintf("%d B", item.RemoteSize)
			}

			rows = append(rows, []string{
				item.RelPath,
				localSizeStr,
				remoteSizeStr,
				string(item.Status),
				item.Reason,
			})
		}

		fmt.Printf("\nDiff Audit for Local [%s] vs s3://%s/%s\n", localDir, bucket, prefix)
		ui.RenderTable(nil, headers, rows)

		fmt.Printf("\nSummary: IN_SYNC: %d | MISSING_IN_S3: %d | MODIFIED: %d | ORPHAN_IN_S3: %d\n",
			counts[sync.StatusInSync],
			counts[sync.StatusMissingInS3],
			counts[sync.StatusModified],
			counts[sync.StatusOrphanInS3],
		)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(diffCmd)
}
