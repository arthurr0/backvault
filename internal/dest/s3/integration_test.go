package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	minioUser = "backvaultadmin"
	minioPass = "backvaultadminsecret"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func minioImage() string {
	if img := os.Getenv("BACKVAULT_TEST_MINIO_IMAGE"); img != "" {
		return img
	}
	return "quay.io/minio/minio:latest"
}

func startMinio(t *testing.T) string {
	t.Helper()
	port := freePort(t)
	startContainer(t, "backvault-minio-",
		"-p", publish(port, 9000),
		"-e", "MINIO_ROOT_USER="+minioUser,
		"-e", "MINIO_ROOT_PASSWORD="+minioPass,
		minioImage(), "server", "/data")
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func adminClient(t *testing.T, endpoint string) *awss3.Client {
	t.Helper()
	cfg, err := awsconfig.LoadDefaultConfig(t.Context(),
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(minioUser, minioPass, "")),
		awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		awsconfig.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	)
	if err != nil {
		t.Fatal(err)
	}
	return awss3.NewFromConfig(cfg, func(o *awss3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
}

func waitForBucket(t *testing.T, api *awss3.Client, bucket string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		_, err := api.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String(bucket)})
		cancel()
		if err == nil {
			return
		}
		if strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") || strings.Contains(err.Error(), "BucketAlreadyExists") {
			return
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("minio did not become ready: %v", lastErr)
}

func TestMinioRoundTrip(t *testing.T) {
	requireDocker(t)
	endpoint := startMinio(t)
	api := adminClient(t, endpoint)
	waitForBucket(t, api, "backvault-test")

	cfg := core.Config{
		"endpoint":     endpoint,
		"region":       "us-east-1",
		"bucket":       "backvault-test",
		"prefix":       "servers/db01",
		"access_key":   minioUser,
		"secret_key":   minioPass,
		"path_style":   true,
		"part_size_mb": 5,
		"concurrency":  4,
	}
	client, err := New().Open(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer client.Close()

	if err := client.Test(t.Context()); err != nil {
		t.Fatalf("test: %v", err)
	}

	payload := bytes.Repeat([]byte("backvault-artifact-"), 1024)
	name := "db-nightly/db-nightly-20240101-000000.dump"
	if err := client.Put(t.Context(), name, bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("put: %v", err)
	}

	st, err := client.Stat(t.Context(), name)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size != int64(len(payload)) {
		t.Errorf("stat size %d, want %d", st.Size, len(payload))
	}

	rc, err := client.Get(t.Context(), name)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Error("downloaded bytes differ from the uploaded ones")
	}

	items, err := client.List(t.Context(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].Path != name {
		t.Errorf("list returned %v", items)
	}
	if items[0].Size != int64(len(payload)) {
		t.Errorf("list size %d", items[0].Size)
	}

	head, err := api.HeadObject(t.Context(), &awss3.HeadObjectInput{
		Bucket: aws.String("backvault-test"),
		Key:    aws.String("servers/db01/" + name),
	})
	if err != nil {
		t.Fatalf("the object is not stored under the configured prefix: %v", err)
	}
	if aws.ToInt64(head.ContentLength) != int64(len(payload)) {
		t.Errorf("stored size %d", aws.ToInt64(head.ContentLength))
	}

	if err := client.Delete(t.Context(), name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := client.Delete(t.Context(), name); err != nil {
		t.Errorf("deleting a missing object should return nil, got %v", err)
	}
	if _, err := client.Stat(t.Context(), name); err == nil {
		t.Error("the object should be gone")
	}
}

func TestMinioMultipartUpload(t *testing.T) {
	requireDocker(t)
	endpoint := startMinio(t)
	waitForBucket(t, adminClient(t, endpoint), "backvault-multipart")

	cfg := core.Config{
		"endpoint": endpoint, "bucket": "backvault-multipart", "access_key": minioUser,
		"secret_key": minioPass, "path_style": true, "part_size_mb": 5, "concurrency": 4,
	}
	client, err := New().Open(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer client.Close()

	size := int64(13 * 1024 * 1024)
	if err := client.Put(t.Context(), "big.bin", newPattern(size), size); err != nil {
		t.Fatalf("put: %v", err)
	}
	st, err := client.Stat(t.Context(), "big.bin")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size != size {
		t.Fatalf("stat size %d, want %d", st.Size, size)
	}
	rc, err := client.Get(t.Context(), "big.bin")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()
	want := newPattern(size)
	if !sameStream(t, rc, want) {
		t.Error("multipart upload round trip mismatch")
	}
}

func TestMinioBadCredentials(t *testing.T) {
	requireDocker(t)
	endpoint := startMinio(t)
	waitForBucket(t, adminClient(t, endpoint), "backvault-auth")
	client, err := New().Open(t.Context(), core.Config{
		"endpoint": endpoint, "bucket": "backvault-auth", "access_key": minioUser,
		"secret_key": "wrong-secret", "path_style": true,
	}, testLogger())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer client.Close()
	if err := client.Test(t.Context()); err == nil {
		t.Error("wrong credentials should fail the test")
	}
}

type pattern struct {
	remaining int64
	n         byte
}

func newPattern(size int64) io.Reader { return &pattern{remaining: size} }

func (p *pattern) Read(b []byte) (int, error) {
	if p.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(b))
	if n > p.remaining {
		n = p.remaining
	}
	for i := int64(0); i < n; i++ {
		b[i] = p.n
		p.n++
	}
	p.remaining -= n
	return int(n), nil
}

func sameStream(t *testing.T, a io.Reader, b io.Reader) bool {
	t.Helper()
	bufA := make([]byte, 64*1024)
	bufB := make([]byte, 64*1024)
	for {
		na, errA := io.ReadFull(a, bufA)
		nb, errB := io.ReadFull(b, bufB)
		if na != nb || !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false
		}
		if errA != nil || errB != nil {
			return (errA == io.EOF || errA == io.ErrUnexpectedEOF) == (errB == io.EOF || errB == io.ErrUnexpectedEOF)
		}
	}
}
