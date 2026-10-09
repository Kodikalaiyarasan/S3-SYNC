package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/cobra"

	"s3-sync/pkg/s3client"
	"s3-sync/pkg/ui"
)

var quotaTopFiles int

type ObjectMeta struct {
	Key          string
	Size         int64
	StorageClass s3types.ObjectStorageClass
}

var quotaCmd = &cobra.Command{
	Use:   "quota s3://<bucket-name>/<prefix>",
	Short: "Bucket size and cost analyzer",
	Long:  `Aggregates total object counts, total volume size, storage class distribution, and estimated monthly costs.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s3URI := args[0]
		bucket, prefix, err := s3client.ParseS3URI(s3URI)
		if err != nil {
			return err
		}

		ctx := context.Background()
		client, err := GetS3Client(ctx)
		if err != nil {
			return fmt.Errorf("failed to initialize S3 client: %w", err)
		}

		cleanPrefix := strings.TrimPrefix(prefix, "/")
		input := &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket),
		}
		if cleanPrefix != "" {
			input.Prefix = aws.String(cleanPrefix)
		}

		var objects []ObjectMeta
		var totalBytes int64
		storageClassCounts := make(map[string]int)
		storageClassBytes := make(map[string]int64)

		paginator := s3.NewListObjectsV2Paginator(client, input)
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return fmt.Errorf("failed listing objects for quota: %w", err)
			}

			for _, item := range page.Contents {
				size := aws.ToInt64(item.Size)
				sc := string(item.StorageClass)
				if sc == "" {
					sc = "STANDARD"
				}

				totalBytes += size
				storageClassCounts[sc]++
				storageClassBytes[sc] += size

				objects = append(objects, ObjectMeta{
					Key:          aws.ToString(item.Key),
					Size:         size,
					StorageClass: item.StorageClass,
				})
			}
		}

		// Sort objects by size descending
		sort.Slice(objects, func(i, j int) bool {
			return objects[i].Size > objects[j].Size
		})

		fmt.Printf("\n=== Bucket Quota & Analysis for s3://%s/%s ===\n", bucket, prefix)
		fmt.Printf("Total Objects: %d\n", len(objects))
		fmt.Printf("Total Volume:  %s (%d bytes)\n", ui.FormatBytes(totalBytes), totalBytes)

		// Storage class distribution table
		scHeaders := []string{"STORAGE CLASS", "OBJECT COUNT", "TOTAL SIZE", "EST. MONTHLY COST"}
		var scRows [][]string

		totalGB := float64(totalBytes) / (1024 * 1024 * 1024)
		var totalEstCost float64

		for sc, count := range storageClassCounts {
			bytes := storageClassBytes[sc]
			scGB := float64(bytes) / (1024 * 1024 * 1024)

			// Standard AWS S3 Pricing Estimates per GB/mo
			rate := 0.023 // STANDARD default $0.023 / GB
			switch sc {
			case "GLACIER":
				rate = 0.004
			case "DEEP_ARCHIVE":
				rate = 0.00099
			case "INTELLIGENT_TIERING":
				rate = 0.023
			case "STANDARD_IA":
				rate = 0.0125
			case "ONEZONE_IA":
				rate = 0.010
			}

			cost := scGB * rate
			totalEstCost += cost

			scRows = append(scRows, []string{
				sc,
				fmt.Sprintf("%d", count),
				ui.FormatBytes(bytes),
				fmt.Sprintf("$%.4f (@ $%.4f/GB)", cost, rate),
			})
		}

		fmt.Println("\n[Storage Class Breakdown]")
		ui.RenderTable(nil, scHeaders, scRows)
		fmt.Printf("Estimated Total Monthly Storage Cost: $%.4f (based on standard AWS S3 rates for %.2f GB)\n", totalEstCost, totalGB)

		// Top N files
		if quotaTopFiles > 0 && len(objects) > 0 {
			topLimit := quotaTopFiles
			if topLimit > len(objects) {
				topLimit = len(objects)
			}

			fmt.Printf("\n[Top %d Largest Files]\n", topLimit)
			topHeaders := []string{"RANK", "FILE KEY", "SIZE"}
			var topRows [][]string
			for i := 0; i < topLimit; i++ {
				topRows = append(topRows, []string{
					fmt.Sprintf("#%d", i+1),
					objects[i].Key,
					ui.FormatBytes(objects[i].Size),
				})
			}
			ui.RenderTable(nil, topHeaders, topRows)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(quotaCmd)

	quotaCmd.Flags().IntVar(&quotaTopFiles, "top-files", 10, "Show the top N largest files")
}
