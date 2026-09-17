package s3

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const (
	defaultPartSizeMB = 16
	defaultConcurrent = 4
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { dest.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:        "s3",
		Label:       "S3-compatible storage",
		Description: "Object storage that speaks the S3 API: AWS S3, MinIO, Backblaze B2, Wasabi, Cloudflare R2 or Hetzner Object Storage.",
		Icon:        "cloud",
		Category:    "Object storage",
		Capabilities: []string{
			core.CapTest,
			core.CapBrowse,
		},
		Fields: []core.Field{
			{
				Name: "endpoint", Label: "Endpoint", Type: core.FieldString, Group: "Connection",
				Placeholder: "https://fsn1.your-objectstorage.com",
				Help: "Leave empty for AWS S3. MinIO: http://minio:9000 with path style on. " +
					"Backblaze B2: https://s3.us-west-004.backblazeb2.com. Wasabi: https://s3.eu-central-1.wasabisys.com. " +
					"Cloudflare R2: https://<account-id>.r2.cloudflarestorage.com with region auto. " +
					"Hetzner Object Storage: https://<location>.your-objectstorage.com.",
			},
			{
				Name: "region", Label: "Region", Type: core.FieldString, Default: "us-east-1",
				Group: "Connection", Placeholder: "eu-central-1",
				Help: "Required by the signature even when the provider ignores it. Cloudflare R2 uses auto.",
			},
			{
				Name: "bucket", Label: "Bucket", Type: core.FieldString, Required: true, Group: "Connection",
				Placeholder: "backvault-backups",
			},
			{
				Name: "prefix", Label: "Prefix", Type: core.FieldString, Group: "Connection",
				Placeholder: "servers/db01",
				Help:        "Optional folder inside the bucket. Artifact paths are stored below it.",
			},
			{
				Name: "access_key", Label: "Access key ID", Type: core.FieldString, Group: "Credentials",
				Placeholder: "AKIA...",
				Help:        "Leave both key fields empty to use the ambient AWS credential chain (environment, shared config, instance role).",
			},
			{
				Name: "secret_key", Label: "Secret access key", Type: core.FieldSecret, Secret: true,
				Group: "Credentials",
			},
			{
				Name: "path_style", Label: "Path-style addressing", Type: core.FieldBool, Default: false,
				Group: "Options",
				Help:  "Turn on for MinIO and most self-hosted gateways. AWS, R2 and Wasabi work with the default virtual-host style.",
			},
			{
				Name: "storage_class", Label: "Storage class", Type: core.FieldSelect, Default: "",
				Group: "Options",
				Options: []core.FieldOption{
					{Value: "", Label: "Provider default"},
					{Value: "STANDARD", Label: "Standard"},
					{Value: "STANDARD_IA", Label: "Standard, infrequent access"},
					{Value: "ONEZONE_IA", Label: "One zone, infrequent access"},
					{Value: "INTELLIGENT_TIERING", Label: "Intelligent tiering"},
					{Value: "GLACIER_IR", Label: "Glacier instant retrieval"},
					{Value: "GLACIER", Label: "Glacier flexible retrieval"},
					{Value: "DEEP_ARCHIVE", Label: "Glacier deep archive"},
				},
				Help: "Archive classes are cheap to store and slow or expensive to restore. Verify and restore will not work until an object is rehydrated.",
			},
			{
				Name: "sse", Label: "Server-side encryption", Type: core.FieldSelect, Default: "none",
				Group: "Options",
				Options: []core.FieldOption{
					{Value: "none", Label: "None"},
					{Value: "AES256", Label: "AES256 (provider managed)"},
					{Value: "aws:kms", Label: "AWS KMS"},
				},
				Help: "Backvault can also encrypt artifacts itself with age, which protects them from the storage provider as well.",
			},
			{
				Name: "sse_kms_key_id", Label: "KMS key id", Type: core.FieldString, Group: "Options",
				ShowIf: map[string]any{"sse": "aws:kms"}, Placeholder: "arn:aws:kms:eu-central-1:...",
			},
			{
				Name: "part_size_mb", Label: "Multipart part size (MiB)", Type: core.FieldInt,
				Default: defaultPartSizeMB, Group: "Advanced", Advanced: true,
				Help: "Minimum 5 MiB. Larger parts mean fewer requests and more memory per upload.",
			},
			{
				Name: "concurrency", Label: "Upload concurrency", Type: core.FieldInt, Default: defaultConcurrent,
				Group: "Advanced", Advanced: true,
			},
			{
				Name: "skip_tls_verify", Label: "Skip certificate verification", Type: core.FieldBool,
				Default: false, Group: "Advanced", Advanced: true,
				Help: "Only for self-signed endpoints on a trusted network.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	if endpoint := cfg.String("endpoint"); endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("endpoint must be a full url such as https://s3.example.com")
		}
	}
	if (cfg.Has("access_key") && !cfg.Has("secret_key")) || (!cfg.Has("access_key") && cfg.Has("secret_key")) {
		return fmt.Errorf("set both the access key and the secret key, or neither")
	}
	if n := cfg.Int("part_size_mb", defaultPartSizeMB); n < 5 {
		return fmt.Errorf("multipart part size must be at least 5 MiB")
	}
	if n := cfg.Int("concurrency", defaultConcurrent); n < 1 || n > 64 {
		return fmt.Errorf("upload concurrency must be between 1 and 64")
	}
	switch cfg.StringOr("sse", "none") {
	case "", "none", "AES256", "aws:kms":
	default:
		return fmt.Errorf("server-side encryption must be none, AES256 or aws:kms")
	}
	return nil
}

func (d *Driver) Open(ctx context.Context, cfg core.Config, log *slog.Logger) (dest.Client, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	region := cfg.StringOr("region", "us-east-1")
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
		awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		awsconfig.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	}
	if cfg.Has("access_key") {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.String("access_key"), cfg.String("secret_key"), "")))
	}
	if cfg.Bool("skip_tls_verify", false) {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		opts = append(opts, awsconfig.WithHTTPClient(&http.Client{Transport: transport, Timeout: 0}))
		log.Warn("tls certificate verification is disabled for this s3 destination")
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws configuration: %w", err)
	}
	api := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		if endpoint := cfg.String("endpoint"); endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = cfg.Bool("path_style", false)
	})
	partSize := int64(cfg.Int("part_size_mb", defaultPartSizeMB)) * 1024 * 1024
	return &client{
		api:         api,
		bucket:      cfg.String("bucket"),
		prefix:      strings.Trim(cfg.String("prefix"), "/"),
		storage:     cfg.String("storage_class"),
		sse:         cfg.StringOr("sse", "none"),
		kmsKey:      cfg.String("sse_kms_key_id"),
		partSize:    partSize,
		concurrency: cfg.Int("concurrency", defaultConcurrent),
		log:         log,
	}, nil
}

type client struct {
	api         *awss3.Client
	bucket      string
	prefix      string
	storage     string
	sse         string
	kmsKey      string
	partSize    int64
	concurrency int
	log         *slog.Logger
}

func (c *client) key(p string) string {
	clean := path.Clean("/" + strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "/"))
	clean = strings.TrimPrefix(clean, "/")
	if c.prefix == "" {
		return clean
	}
	if clean == "" || clean == "." {
		return c.prefix
	}
	return c.prefix + "/" + clean
}

func (c *client) relative(key string) string {
	if c.prefix == "" {
		return key
	}
	return strings.TrimPrefix(strings.TrimPrefix(key, c.prefix), "/")
}

func (c *client) Put(ctx context.Context, p string, r io.Reader, size int64) error {
	uploader := manager.NewUploader(c.api, func(u *manager.Uploader) {
		u.PartSize = c.partSize
		u.Concurrency = c.concurrency
		u.LeavePartsOnError = false
	})
	input := &awss3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.key(p)),
		Body:   r,
	}
	if size > 0 {
		input.ContentLength = aws.Int64(size)
	}
	if c.storage != "" {
		input.StorageClass = s3types.StorageClass(c.storage)
	}
	switch c.sse {
	case "AES256":
		input.ServerSideEncryption = s3types.ServerSideEncryptionAes256
	case "aws:kms":
		input.ServerSideEncryption = s3types.ServerSideEncryptionAwsKms
		if c.kmsKey != "" {
			input.SSEKMSKeyId = aws.String(c.kmsKey)
		}
	}
	if _, err := uploader.Upload(ctx, input); err != nil {
		return fmt.Errorf("upload %s to s3://%s/%s: %w", p, c.bucket, c.key(p), clean(err))
	}
	return nil
}

func (c *client) Get(ctx context.Context, p string) (io.ReadCloser, error) {
	out, err := c.api.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.key(p)),
	})
	if err != nil {
		return nil, fmt.Errorf("download s3://%s/%s: %w", c.bucket, c.key(p), clean(err))
	}
	return out.Body, nil
}

func (c *client) Delete(ctx context.Context, p string) error {
	_, err := c.api.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.key(p)),
	})
	if err != nil {
		if notFound(err) {
			return nil
		}
		return fmt.Errorf("delete s3://%s/%s: %w", c.bucket, c.key(p), clean(err))
	}
	return nil
}

func (c *client) List(ctx context.Context, prefix string) ([]dest.Object, error) {
	full := c.key(prefix)
	if prefix == "" {
		full = c.prefix
	}
	input := &awss3.ListObjectsV2Input{Bucket: aws.String(c.bucket)}
	if full != "" {
		input.Prefix = aws.String(full)
	}
	out := []dest.Object{}
	pager := awss3.NewListObjectsV2Paginator(c.api, input)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list s3://%s/%s: %w", c.bucket, full, clean(err))
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			rel := c.relative(key)
			if rel == "" {
				continue
			}
			o := dest.Object{Path: rel, Size: aws.ToInt64(obj.Size)}
			if obj.LastModified != nil {
				o.ModTime = obj.LastModified.UTC()
			}
			out = append(out, o)
		}
	}
	return out, nil
}

func (c *client) Stat(ctx context.Context, p string) (*dest.Object, error) {
	out, err := c.api.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.key(p)),
	})
	if err != nil {
		return nil, fmt.Errorf("stat s3://%s/%s: %w", c.bucket, c.key(p), clean(err))
	}
	obj := &dest.Object{Path: strings.TrimPrefix(p, "/"), Size: aws.ToInt64(out.ContentLength)}
	if out.LastModified != nil {
		obj.ModTime = out.LastModified.UTC()
	}
	return obj, nil
}

func (c *client) Test(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := c.api.HeadBucket(ctx, &awss3.HeadBucketInput{Bucket: aws.String(c.bucket)}); err == nil {
		return nil
	}
	input := &awss3.ListObjectsV2Input{Bucket: aws.String(c.bucket), MaxKeys: aws.Int32(1)}
	if c.prefix != "" {
		input.Prefix = aws.String(c.prefix)
	}
	if _, err := c.api.ListObjectsV2(ctx, input); err != nil {
		return fmt.Errorf("bucket %s is not reachable: %w", c.bucket, clean(err))
	}
	return nil
}

func (c *client) Close() error { return nil }

func notFound(err error) bool {
	var nsk *s3types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *s3types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		}
	}
	return false
}

func clean(err error) error {
	if err == nil {
		return nil
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		return fmt.Errorf("%s: %s", api.ErrorCode(), api.ErrorMessage())
	}
	return err
}

var (
	_ dest.Driver = (*Driver)(nil)
	_ dest.Client = (*client)(nil)
)
