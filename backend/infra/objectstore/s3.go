package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"strings"
	"time"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	maxImageSize       int64 = 10 << 20
	thumbnailMaxPixels       = 512
)

var (
	ErrInvalidImage  = errors.New("invalid image: only readable JPEG and PNG files are supported")
	ErrImageTooLarge = errors.New("image exceeds the 10 MiB limit")
)

type S3ImageStoreConfig struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKey       string
	SecretKey       string
	UsePathStyle    bool
	KeyPrefix       string
	PresignValidity time.Duration
}

type S3ImageStore struct {
	client          *minio.Client
	bucket          string
	keyPrefix       string
	presignValidity time.Duration
}

func NewS3ImageStore(cfg S3ImageStoreConfig) (*S3ImageStore, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("s3 endpoint, bucket, access key and secret key are required")
	}
	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid s3 endpoint %q", cfg.Endpoint)
	}
	lookup := minio.BucketLookupDNS
	if cfg.UsePathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(parsed.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Region:       cfg.Region,
		Secure:       parsed.Scheme == "https",
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}
	client.SetAppInfo("guide-flow", "stage-3")
	validity := cfg.PresignValidity
	if validity <= 0 {
		validity = 15 * time.Minute
	}
	return &S3ImageStore{client: client, bucket: cfg.Bucket, keyPrefix: strings.Trim(cfg.KeyPrefix, "/"), presignValidity: validity}, nil
}

func (s *S3ImageStore) Save(ctx context.Context, id string, source io.Reader) error {
	if err := validateObjectID(id); err != nil {
		return err
	}
	data, contentType, width, height, err := readAndValidateImage(source)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	metadata := map[string]string{
		"sha256": hex.EncodeToString(hash[:]),
		"width":  fmt.Sprintf("%d", width),
		"height": fmt.Sprintf("%d", height),
	}
	key := s.objectKey(id, "original")
	if _, err = s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: contentType, UserMetadata: metadata}); err != nil {
		return fmt.Errorf("put original image: %w", err)
	}
	thumbnail, err := makeThumbnail(data, width, height)
	if err != nil {
		_ = s.deleteObjects(ctx, id)
		return fmt.Errorf("create thumbnail: %w", err)
	}
	if _, err = s.client.PutObject(ctx, s.bucket, s.objectKey(id, "thumbnail"), bytes.NewReader(thumbnail), int64(len(thumbnail)), minio.PutObjectOptions{ContentType: "image/jpeg", UserMetadata: metadata}); err != nil {
		_ = s.deleteObjects(ctx, id)
		return fmt.Errorf("put thumbnail: %w", err)
	}
	return nil
}

func (s *S3ImageStore) Open(ctx context.Context, id string) (io.ReadCloser, error) {
	if err := validateObjectID(id); err != nil {
		return nil, err
	}
	object, err := s.client.GetObject(ctx, s.bucket, s.objectKey(id, "original"), minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get image: %w", err)
	}
	// GetObject 是惰性请求，先 Stat 才能把不存在的对象尽早转换成明确错误。
	if _, err = object.Stat(); err != nil {
		_ = object.Close()
		return nil, fmt.Errorf("stat image: %w", err)
	}
	return object, nil
}

func (s *S3ImageStore) Delete(ctx context.Context, id string) error {
	if err := validateObjectID(id); err != nil {
		return err
	}
	return s.deleteObjects(ctx, id)
}

func (s *S3ImageStore) Presign(ctx context.Context, id string, expiry time.Duration) (string, error) {
	if err := validateObjectID(id); err != nil {
		return "", err
	}
	if expiry <= 0 {
		expiry = s.presignValidity
	}
	if expiry > 7*24*time.Hour {
		return "", errors.New("presign expiry cannot exceed 7 days")
	}
	url, err := s.client.PresignedGetObject(ctx, s.bucket, s.objectKey(id, "original"), expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign image: %w", err)
	}
	return url.String(), nil
}

func (s *S3ImageStore) References(_ context.Context, id string) (domain.ImageReferences, error) {
	if err := validateObjectID(id); err != nil {
		return domain.ImageReferences{}, err
	}
	return domain.ImageReferences{
		OriginalObjectKey:  s.objectKey(id, "original"),
		ThumbnailObjectKey: s.objectKey(id, "thumbnail"),
	}, nil
}

func (s *S3ImageStore) deleteObjects(ctx context.Context, id string) error {
	var firstErr error
	for _, variant := range []string{"original", "thumbnail"} {
		if err := s.client.RemoveObject(ctx, s.bucket, s.objectKey(id, variant), minio.RemoveObjectOptions{}); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("delete %s image: %w", variant, err)
		}
	}
	return firstErr
}

func (s *S3ImageStore) objectKey(id, variant string) string {
	parts := []string{}
	if s.keyPrefix != "" {
		parts = append(parts, s.keyPrefix)
	}
	extension := ".bin"
	if variant == "thumbnail" {
		extension = ".jpg"
	}
	parts = append(parts, "lessons", id, variant+extension)
	return strings.Join(parts, "/")
}

func validateObjectID(id string) error {
	if id == "" || strings.ContainsAny(id, `/\\`) || id == "." || id == ".." || strings.ContainsRune(id, 0) {
		return errors.New("invalid image object id")
	}
	return nil
}

func readAndValidateImage(source io.Reader) ([]byte, string, int, int, error) {
	limited := io.LimitReader(source, maxImageSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", 0, 0, fmt.Errorf("read image: %w", err)
	}
	if int64(len(data)) > maxImageSize {
		return nil, "", 0, 0, ErrImageTooLarge
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || config.Width <= 0 || config.Height <= 0 {
		return nil, "", 0, 0, ErrInvalidImage
	}
	contentType := "image/jpeg"
	if format == "png" {
		contentType = "image/png"
	}
	return data, contentType, config.Width, config.Height, nil
}

func makeThumbnail(data []byte, width, height int) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	scale := 1.0
	if width > thumbnailMaxPixels || height > thumbnailMaxPixels {
		scale = minFloat(float64(thumbnailMaxPixels)/float64(width), float64(thumbnailMaxPixels)/float64(height))
	}
	dstWidth, dstHeight := maxInt(1, int(float64(width)*scale)), maxInt(1, int(float64(height)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	for y := 0; y < dstHeight; y++ {
		for x := 0; x < dstWidth; x++ {
			dst.Set(x, y, src.At(x*width/dstWidth, y*height/dstHeight))
		}
	}
	var out bytes.Buffer
	if err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
