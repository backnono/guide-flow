package objectstore

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestReadAndValidateImage(t *testing.T) {
	var encoded bytes.Buffer
	src := image.NewRGBA(image.Rect(0, 0, 2, 3))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	data, contentType, width, height, err := readAndValidateImage(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || contentType != "image/png" || width != 2 || height != 3 {
		t.Fatalf("unexpected image metadata: len=%d type=%s size=%dx%d", len(data), contentType, width, height)
	}
}

func TestReadAndValidateImageRejectsInvalidAndOversizedInput(t *testing.T) {
	if _, _, _, _, err := readAndValidateImage(strings.NewReader("not an image")); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("expected invalid image error, got %v", err)
	}
	oversized := bytes.Repeat([]byte{'x'}, int(maxImageSize)+1)
	if _, _, _, _, err := readAndValidateImage(io.LimitReader(bytes.NewReader(oversized), maxImageSize+1)); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("expected oversized image error, got %v", err)
	}
}

func TestMakeThumbnailCapsDimensions(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2048, 1024))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	thumbnail, err := makeThumbnail(encoded.Bytes(), 2048, 1024)
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(thumbnail))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != thumbnailMaxPixels || config.Height != thumbnailMaxPixels/2 {
		t.Fatalf("unexpected thumbnail size: %dx%d", config.Width, config.Height)
	}
}

func TestMemoryImageStoreDeleteAndMissingObject(t *testing.T) {
	store := NewMemoryImageStore()
	store.Put("lesson-1", []byte("image"))
	if err := store.Delete(context.Background(), "lesson-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), "lesson-1"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("expected missing object error, got %v", err)
	}
}

func TestS3ImageStoreRejectsUnsafeObjectID(t *testing.T) {
	store := &S3ImageStore{keyPrefix: "uploads"}
	if err := store.Delete(context.Background(), "../escape"); err == nil {
		t.Fatal("expected unsafe object id to be rejected")
	}
	if got := store.objectKey("lesson-1", "original"); got != "uploads/lessons/lesson-1/original.bin" {
		t.Fatalf("unexpected object key: %s", got)
	}
}

func TestS3ImageStoreRoundTrip(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_TEST_ENDPOINT is not configured")
	}
	store, err := NewS3ImageStore(S3ImageStoreConfig{
		Endpoint: endpoint, Region: envOr("S3_TEST_REGION", "us-east-1"), Bucket: envOr("S3_TEST_BUCKET", "guide-flow-test"),
		AccessKey: envOr("S3_TEST_ACCESS_KEY", "minioadmin"), SecretKey: envOr("S3_TEST_SECRET_KEY", "minioadmin"),
		UsePathStyle: true, KeyPrefix: "stage-3-test", PresignValidity: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	exists, err := store.client.BucketExists(ctx, store.bucket)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		if err = store.client.MakeBucket(ctx, store.bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
			t.Fatal(err)
		}
	}

	var encoded bytes.Buffer
	if err = png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	const id = "stage-3-round-trip"
	defer store.Delete(ctx, id)
	if err = store.Save(ctx, id, bytes.NewReader(encoded.Bytes())); err != nil {
		t.Fatal(err)
	}
	opened, err := store.Open(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(opened)
	_ = opened.Close()
	if err != nil || !bytes.Equal(got, encoded.Bytes()) {
		t.Fatalf("round trip mismatch: err=%v bytes=%d", err, len(got))
	}
	if _, err = store.Presign(ctx, id, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err = store.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
