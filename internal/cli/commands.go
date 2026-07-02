package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const defaultBaseURL = "https://lunars.dev"

type Options struct {
	BaseURL    string
	Cookie     string
	CookieFile string
	Force      bool
	JSON       bool
	LogLevel   string
	Output     string
	Resume     bool
	Search     string
	Type       string
}

func NewRootCommand(out, errOut io.Writer, version string) *cobra.Command {
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}

	settings := viper.New()
	settings.SetEnvPrefix("LUNARS")
	settings.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	settings.AutomaticEnv()

	root := &cobra.Command{
		Use:           "lunars",
		Short:         "Download authorized files from lunars.dev",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)

	root.PersistentFlags().String("base-url", defaultBaseURL, "lunars.dev base URL")
	root.PersistentFlags().String("cookie", "", "raw Cookie header from an authorized lunars.dev session")
	root.PersistentFlags().String("cookie-file", "", "Cookie header, session token, or Netscape cookie export path")
	root.PersistentFlags().String("config", "", "config file path; defaults to XDG config home")
	root.PersistentFlags().String("log-level", "warn", "log level: trace, debug, info, warn, error")
	bindFlag(settings, root.PersistentFlags(), "base-url")
	bindFlag(settings, root.PersistentFlags(), "cookie")
	bindFlag(settings, root.PersistentFlags(), "cookie-file")
	bindFlag(settings, root.PersistentFlags(), "config")
	bindFlag(settings, root.PersistentFlags(), "log-level")
	loadConfig := configLoader(settings)

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List available firmware records",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			opts := optionsFromViper(settings)
			opts.JSON, _ = cmd.Flags().GetBool("json")
			opts.Search, _ = cmd.Flags().GetString("search")
			opts.Type, _ = cmd.Flags().GetString("type")
			opts.Type = strings.ToLower(opts.Type)
			client, err := clientFromOptions(opts, errOut)
			if err != nil {
				return err
			}
			return RunList(cmd.Context(), client, opts, out)
		},
	}
	listCmd.Flags().Bool("json", false, "emit JSON")
	listCmd.Flags().String("search", "", "filter records by search text")
	listCmd.Flags().String("type", "", "filter records by file type")

	limitCmd := &cobra.Command{
		Use:   "limit",
		Short: "Show monthly download usage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			opts := optionsFromViper(settings)
			opts.JSON, _ = cmd.Flags().GetBool("json")
			client, err := clientFromOptions(opts, errOut)
			if err != nil {
				return err
			}
			return RunLimit(cmd.Context(), client, opts, out)
		},
	}
	limitCmd.Flags().Bool("json", false, "emit JSON")

	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage lunars.dev authentication",
	}
	authCheckCmd := &cobra.Command{
		Use:   "check",
		Short: "Validate the configured lunars.dev session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			opts := optionsFromViper(settings)
			opts.JSON, _ = cmd.Flags().GetBool("json")
			client, err := clientFromOptions(opts, errOut)
			if err != nil {
				return err
			}
			return RunAuthCheck(cmd.Context(), client, opts, out)
		},
	}
	authCheckCmd.Flags().Bool("json", false, "emit JSON")
	authCmd.AddCommand(authCheckCmd)

	downloadCmd := &cobra.Command{
		Use:   "download <version|signature|path|url>",
		Short: "Download a file through lunars.dev",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			opts := optionsFromViper(settings)
			opts.Output, _ = cmd.Flags().GetString("output")
			opts.Force, _ = cmd.Flags().GetBool("force")
			opts.Resume, _ = cmd.Flags().GetBool("resume")
			client, err := clientFromOptions(opts, errOut)
			if err != nil {
				return err
			}
			return RunDownload(cmd.Context(), client, opts, args[0], out, ".")
		},
	}
	downloadCmd.Flags().StringP("output", "o", "", "output file or existing directory")
	downloadCmd.Flags().Bool("force", false, "overwrite an existing output file")
	downloadCmd.Flags().Bool("resume", false, "resume a partial .part download when the server supports ranges")

	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage lunars CLI configuration",
	}
	configPathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _, err := configPath(settings)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(out, configFile)
			return err
		},
	}
	configShowCmd := &cobra.Command{
		Use:   "show",
		Short: "Print effective S3 configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			configFile, _, err := configPath(settings)
			if err != nil {
				return err
			}
			return ShowS3Config(configFile, s3ConfigFromSettings(settings), out)
		},
	}
	configInitCmd := &cobra.Command{
		Use:   "init",
		Short: "Write an S3 config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _, err := configPath(settings)
			if err != nil {
				return err
			}

			uploadOpts := S3UploadOptions{}
			uploadOpts.Bucket, _ = cmd.Flags().GetString("bucket")
			uploadOpts.Prefix, _ = cmd.Flags().GetString("prefix")
			uploadOpts.EndpointURL, _ = cmd.Flags().GetString("endpoint-url")
			uploadOpts.Region, _ = cmd.Flags().GetString("region")
			uploadOpts.PathStyle, _ = cmd.Flags().GetBool("path-style")
			force, _ := cmd.Flags().GetBool("force")

			if err := WriteS3Config(configFile, uploadOpts, force); err != nil {
				return err
			}
			_, err = fmt.Fprintf(out, "Wrote %s\n", configFile)
			return err
		},
	}
	configInitCmd.Flags().String("bucket", "", "S3 bucket name")
	configInitCmd.Flags().String("prefix", "lunars/", "S3 object key prefix")
	configInitCmd.Flags().String("endpoint-url", "", "S3-compatible endpoint URL")
	configInitCmd.Flags().String("region", "", "AWS region or S3-compatible region")
	configInitCmd.Flags().Bool("path-style", true, "use path-style bucket addressing")
	configInitCmd.Flags().Bool("force", false, "overwrite an existing config file")
	configSetCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set one S3 config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			configFile, _, err := configPath(settings)
			if err != nil {
				return err
			}
			opts := s3ConfigFromSettings(settings)
			if err := SetS3ConfigValue(&opts, args[0], args[1]); err != nil {
				return err
			}
			if err := WriteS3Config(configFile, opts, true); err != nil {
				return err
			}
			_, err = fmt.Fprintf(out, "Updated %s\n", configFile)
			return err
		},
	}
	configCmd.AddCommand(configPathCmd, configShowCmd, configInitCmd, configSetCmd)

	uploadCmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload lunars.dev files to external storage",
	}
	s3Cmd := &cobra.Command{
		Use:   "s3 <version|signature|path|url>",
		Short: "Stream a lunars.dev file into S3-compatible storage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			opts := optionsFromViper(settings)
			client, err := clientFromOptions(opts, errOut)
			if err != nil {
				return err
			}

			uploadOpts := S3UploadOptions{}
			uploadOpts.Bucket = stringFlagOrConfig(cmd, settings, "bucket", "s3.bucket", "")
			uploadOpts.Key = stringFlagOrConfig(cmd, settings, "key", "s3.key", "")
			uploadOpts.Prefix = stringFlagOrConfig(cmd, settings, "prefix", "s3.prefix", "lunars/")
			uploadOpts.EndpointURL = stringFlagOrConfig(cmd, settings, "endpoint-url", "s3.endpoint-url", "")
			uploadOpts.Region = stringFlagOrConfig(cmd, settings, "region", "s3.region", "")
			uploadOpts.PathStyle = boolFlagOrConfig(cmd, settings, "path-style", "s3.path-style", true)
			uploadOpts.Execute, _ = cmd.Flags().GetBool("execute")
			yes, _ := cmd.Flags().GetBool("yes")

			var uploader ObjectUploader
			if uploadOpts.Execute {
				if !yes {
					confirmed, err := confirmS3Execute(cmd.InOrStdin(), errOut)
					if err != nil {
						return err
					}
					if !confirmed {
						return errors.New("upload cancelled")
					}
				}
				uploader, err = NewAWSS3Uploader(cmd.Context(), uploadOpts)
				if err != nil {
					return err
				}
			}
			return RunS3Upload(cmd.Context(), client, uploader, uploadOpts, args[0], out)
		},
	}
	s3Cmd.Flags().String("bucket", "", "S3 bucket name")
	s3Cmd.Flags().String("key", "", "exact S3 object key; defaults to prefix plus archive path")
	s3Cmd.Flags().String("prefix", "lunars/", "S3 object key prefix")
	s3Cmd.Flags().String("endpoint-url", "", "S3-compatible endpoint URL")
	s3Cmd.Flags().String("region", "", "AWS region or S3-compatible region")
	s3Cmd.Flags().Bool("path-style", true, "use path-style bucket addressing")
	s3Cmd.Flags().Bool("execute", false, "actually request a signed URL and upload; dry-run by default")
	s3Cmd.Flags().Bool("yes", false, "skip the --execute confirmation prompt")
	uploadCmd.AddCommand(s3Cmd)

	root.AddCommand(authCmd, listCmd, limitCmd, downloadCmd, configCmd, uploadCmd, completionCommand(root, out))
	return root
}

func configLoader(settings *viper.Viper) func() error {
	loaded := false
	return func() error {
		if loaded {
			return nil
		}
		loaded = true

		configFile, explicit, err := configPath(settings)
		if err != nil {
			return err
		}
		if explicit {
			settings.SetConfigFile(configFile)
		} else {
			settings.SetConfigName("config")
			settings.SetConfigType("yaml")
			settings.AddConfigPath(filepath.Dir(configFile))
		}

		if err := settings.ReadInConfig(); err != nil {
			var notFound viper.ConfigFileNotFoundError
			if errors.As(err, &notFound) {
				return nil
			}
			return err
		}
		return nil
	}
}

func configPath(settings *viper.Viper) (string, bool, error) {
	if configFile := settings.GetString("config"); configFile != "" {
		return configFile, true, nil
	}

	configHome, err := os.UserConfigDir()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(configHome, "lunars", "config.yaml"), false, nil
}

func bindFlag(settings *viper.Viper, flags flagSet, key string) {
	if err := settings.BindPFlag(key, flags.Lookup(key)); err != nil {
		panic(err)
	}
}

func optionsFromViper(settings *viper.Viper) Options {
	return Options{
		BaseURL:    settings.GetString("base-url"),
		Cookie:     settings.GetString("cookie"),
		CookieFile: settings.GetString("cookie-file"),
		Force:      settings.GetBool("force"),
		JSON:       settings.GetBool("json"),
		LogLevel:   settings.GetString("log-level"),
		Output:     settings.GetString("output"),
		Resume:     settings.GetBool("resume"),
		Search:     settings.GetString("search"),
		Type:       strings.ToLower(settings.GetString("type")),
	}
}

func stringFlagOrConfig(cmd *cobra.Command, settings *viper.Viper, flagName, configKey, fallback string) string {
	if cmd.Flags().Changed(flagName) {
		value, _ := cmd.Flags().GetString(flagName)
		return value
	}
	if settings.IsSet(configKey) {
		return settings.GetString(configKey)
	}
	return fallback
}

func s3ConfigFromSettings(settings *viper.Viper) S3UploadOptions {
	return S3UploadOptions{
		Bucket:      settings.GetString("s3.bucket"),
		Key:         settings.GetString("s3.key"),
		Prefix:      stringConfigOrFallback(settings, "s3.prefix", "lunars/"),
		EndpointURL: settings.GetString("s3.endpoint-url"),
		Region:      settings.GetString("s3.region"),
		PathStyle:   boolConfigOrFallback(settings, "s3.path-style", true),
	}
}

func stringConfigOrFallback(settings *viper.Viper, key, fallback string) string {
	if settings.IsSet(key) {
		return settings.GetString(key)
	}
	return fallback
}

func boolConfigOrFallback(settings *viper.Viper, key string, fallback bool) bool {
	if settings.IsSet(key) {
		return settings.GetBool(key)
	}
	return fallback
}

func WriteS3Config(configFile string, opts S3UploadOptions, force bool) error {
	opts.Bucket = strings.TrimSpace(opts.Bucket)
	if opts.Bucket == "" {
		return errors.New("--bucket is required")
	}

	if err := os.MkdirAll(filepath.Dir(configFile), 0700); err != nil {
		return err
	}

	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !force {
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(configFile, flags, 0600) // #nosec G304 -- configFile is the user's explicit CLI/XDG config path.
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists; pass --force to overwrite", configFile)
		}
		return err
	}

	var body strings.Builder
	body.WriteString("s3:\n")
	fmt.Fprintf(&body, "  bucket: %s\n", yamlString(opts.Bucket))
	if opts.Key != "" {
		fmt.Fprintf(&body, "  key: %s\n", yamlString(opts.Key))
	}
	if opts.Prefix != "" {
		fmt.Fprintf(&body, "  prefix: %s\n", yamlString(opts.Prefix))
	}
	if opts.EndpointURL != "" {
		fmt.Fprintf(&body, "  endpoint-url: %s\n", yamlString(opts.EndpointURL))
	}
	if opts.Region != "" {
		fmt.Fprintf(&body, "  region: %s\n", yamlString(opts.Region))
	}
	fmt.Fprintf(&body, "  path-style: %t\n", opts.PathStyle)

	if _, err := io.WriteString(file, body.String()); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func ShowS3Config(configFile string, opts S3UploadOptions, out io.Writer) error {
	if _, err := os.Stat(configFile); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, err = fmt.Fprintf(out, "No config file at %s\n", configFile)
			return err
		}
		return err
	}
	_, err := fmt.Fprintf(out, "path: %s\n%s", configFile, RenderS3ConfigYAML(opts))
	return err
}

func SetS3ConfigValue(opts *S3UploadOptions, key, value string) error {
	switch strings.TrimPrefix(strings.ToLower(key), "s3.") {
	case "bucket":
		opts.Bucket = value
	case "key":
		opts.Key = value
	case "prefix":
		opts.Prefix = value
	case "endpoint-url":
		opts.EndpointURL = value
	case "region":
		opts.Region = value
	case "path-style":
		switch strings.ToLower(value) {
		case "true", "1", "yes", "y", "on":
			opts.PathStyle = true
		case "false", "0", "no", "n", "off":
			opts.PathStyle = false
		default:
			return fmt.Errorf("invalid boolean value for %s: %q", key, value)
		}
	default:
		return fmt.Errorf("unsupported config key %q", key)
	}
	return nil
}

func RenderS3ConfigYAML(opts S3UploadOptions) string {
	var body strings.Builder
	body.WriteString("s3:\n")
	if opts.Bucket != "" {
		fmt.Fprintf(&body, "  bucket: %s\n", yamlString(opts.Bucket))
	}
	if opts.Key != "" {
		fmt.Fprintf(&body, "  key: %s\n", yamlString(opts.Key))
	}
	if opts.Prefix != "" {
		fmt.Fprintf(&body, "  prefix: %s\n", yamlString(opts.Prefix))
	}
	if opts.EndpointURL != "" {
		fmt.Fprintf(&body, "  endpoint-url: %s\n", yamlString(opts.EndpointURL))
	}
	if opts.Region != "" {
		fmt.Fprintf(&body, "  region: %s\n", yamlString(opts.Region))
	}
	fmt.Fprintf(&body, "  path-style: %t\n", opts.PathStyle)
	return body.String()
}

func yamlString(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(data)
}

func boolFlagOrConfig(cmd *cobra.Command, settings *viper.Viper, flagName, configKey string, fallback bool) bool {
	if cmd.Flags().Changed(flagName) {
		value, _ := cmd.Flags().GetBool(flagName)
		return value
	}
	if settings.IsSet(configKey) {
		return settings.GetBool(configKey)
	}
	return fallback
}

func clientFromOptions(opts Options, errOut io.Writer) (*Client, error) {
	cookie, err := CookieFromOptions(opts)
	if err != nil {
		return nil, err
	}

	logger, err := loggerFromOptions(opts, errOut)
	if err != nil {
		return nil, err
	}

	parsed, err := url.Parse(opts.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid --base-url %q", opts.BaseURL)
	}

	return NewClient(opts.BaseURL, cookie, logger)
}

func CookieFromOptions(opts Options) (string, error) {
	if opts.Cookie != "" && opts.CookieFile != "" {
		return "", errors.New("use either --cookie or --cookie-file, not both")
	}
	if opts.Cookie != "" {
		return NormalizeCookieHeader(opts.Cookie)
	}
	if opts.CookieFile != "" {
		data, err := os.ReadFile(opts.CookieFile)
		if err != nil {
			return "", err
		}
		return ParseCookieFile(string(data), "lunars.dev")
	}
	return "", errors.New("authentication required; set LUNARS_COOKIE or pass --cookie/--cookie-file from an authorized lunars.dev browser session")
}

func loggerFromOptions(opts Options, errOut io.Writer) (*logrus.Logger, error) {
	logger := logrus.New()
	logger.SetOutput(errOut)
	logger.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})

	level, err := logrus.ParseLevel(opts.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("invalid --log-level %q", opts.LogLevel)
	}
	logger.SetLevel(level)
	return logger, nil
}

func confirmS3Execute(in io.Reader, out io.Writer) (bool, error) {
	_, err := io.WriteString(out, "This will request a signed lunars.dev URL, consume download quota, and upload to S3. Type yes to continue: ")
	if err != nil {
		return false, err
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(line), "yes"), nil
}

func RunList(ctx context.Context, client *Client, opts Options, out io.Writer) error {
	records, err := client.Signatures(ctx)
	if err != nil {
		return err
	}
	records = FilterFirmware(records, opts)

	if opts.JSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(records)
	}

	_, err = io.WriteString(out, RenderFirmwareTable(records))
	return err
}

func RunAuthCheck(ctx context.Context, client *Client, opts Options, out io.Writer) error {
	limit, err := client.Limit(ctx)
	if err != nil {
		return err
	}

	if opts.JSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(limit)
	}

	if limit.Limit == nil {
		_, err = fmt.Fprintf(out, "Authenticated.\n%+v\n", limit)
		return err
	}

	remaining := limit.Limit.AllowedLimit - limit.Limit.CurrentCount
	if remaining < 0 {
		remaining = 0
	}
	_, err = fmt.Fprintf(out, "Authenticated. Downloads: %d/%d used, %d remaining\n", limit.Limit.CurrentCount, limit.Limit.AllowedLimit, remaining)
	return err
}

func RunLimit(ctx context.Context, client *Client, opts Options, out io.Writer) error {
	limit, err := client.Limit(ctx)
	if err != nil {
		return err
	}

	if opts.JSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(limit)
	}

	if limit.Limit == nil {
		_, err = fmt.Fprintf(out, "%+v\n", limit)
		return err
	}

	remaining := limit.Limit.AllowedLimit - limit.Limit.CurrentCount
	if remaining < 0 {
		remaining = 0
	}
	_, err = fmt.Fprintf(out, "Downloads: %d/%d used, %d remaining\n", limit.Limit.CurrentCount, limit.Limit.AllowedLimit, remaining)
	return err
}
