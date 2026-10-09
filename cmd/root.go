package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"s3-sync/pkg/s3client"
)

var (
	cfgFile     string
	endpointURL string
	region      string
	pathStyle   bool

	rootCmd = &cobra.Command{
		Use:   "s3-sync",
		Short: "s3-sync is a high-concurrency cloud object storage mirror utility",
		Long: `s3-sync mirrors local directories to S3-compatible cloud storage providers
(AWS S3, MinIO, Cloudflare R2, Wasabi, DigitalOcean Spaces) and vice versa.
It features parallel worker pools, smart delta detection via ETag/MD5 hashes,
and comprehensive bucket maintenance utilities.`,
	}
)

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.s3-sync.yaml)")
	rootCmd.PersistentFlags().StringVar(&endpointURL, "endpoint-url", "", "Custom S3 endpoint URL (e.g., http://localhost:9000)")
	rootCmd.PersistentFlags().StringVar(&region, "region", "us-east-1", "S3 region")
	rootCmd.PersistentFlags().BoolVar(&pathStyle, "path-style", false, "Force path-style addressing (http://endpoint/bucket/key)")

	// Bind flags to viper
	viper.BindPFlag("endpoint-url", rootCmd.PersistentFlags().Lookup("endpoint-url"))
	viper.BindPFlag("region", rootCmd.PersistentFlags().Lookup("region"))
	viper.BindPFlag("path-style", rootCmd.PersistentFlags().Lookup("path-style"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		// Check explicit conf locations
		for _, candidate := range []string{"conf/config.yaml", "./conf/config.yaml", "config.yaml"} {
			if _, err := os.Stat(candidate); err == nil {
				viper.SetConfigFile(candidate)
				break
			}
		}

		if viper.ConfigFileUsed() == "" {
			viper.SetConfigType("yaml")
			viper.SetConfigName("config")
			viper.AddConfigPath("./conf")
			viper.AddConfigPath("conf")
			viper.AddConfigPath(".")

			if home, err := os.UserHomeDir(); err == nil {
				viper.AddConfigPath(home)
				viper.SetConfigName(".s3-sync")
			}
		}
	}

	// Environment variable bindings
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// Specific bindings for standard AWS & S3 env vars
	viper.BindEnv("endpoint-url", "S3_ENDPOINT_URL")
	viper.BindEnv("region", "AWS_REGION", "AWS_DEFAULT_REGION")
	viper.BindEnv("access-key-id", "AWS_ACCESS_KEY_ID")
	viper.BindEnv("secret-access-key", "AWS_SECRET_ACCESS_KEY")
	viper.BindEnv("session-token", "AWS_SESSION_TOKEN")

	// Read in config file
	_ = viper.ReadInConfig()
}

// GetS3Client creates an S3 client using flags, viper config, and env vars
func GetS3Client(ctx context.Context) (*s3.Client, error) {
	ep := viper.GetString("endpoint-url")
	reg := viper.GetString("region")
	ps := viper.GetBool("path-style")
	ak := viper.GetString("access-key-id")
	sk := viper.GetString("secret-access-key")
	st := viper.GetString("session-token")

	return s3client.NewClient(ctx, s3client.Config{
		Region:          reg,
		EndpointURL:     ep,
		UsePathStyle:    ps,
		AccessKeyID:     ak,
		SecretAccessKey: sk,
		SessionToken:    st,
	})
}
