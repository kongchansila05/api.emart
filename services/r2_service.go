package services

import (
	"context"
	"fmt"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// R2Service handles uploads to Cloudflare R2.
type R2Service struct {
	client     *s3.Client
	bucket     string
	publicBase string // e.g. https://pub-xxx.r2.dev  or your custom domain
}

// NewR2Service constructs an R2Service from env config.
//
// Required env vars (load via os.Getenv or viper):
//
//	R2_ACCOUNT_ID   — Cloudflare account ID
//	R2_ACCESS_KEY   — R2 access key ID
//	R2_SECRET_KEY   — R2 secret access key
//	R2_BUCKET       — bucket name
//	R2_PUBLIC_URL   — public base URL  (e.g. https://pub-xxx.r2.dev)
func NewR2Service(accountID, accessKey, secretKey, bucket, publicBase string) (*R2Service, error) {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("auto"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
		config.WithEndpointResolverWithOptions(
			aws.EndpointResolverWithOptionsFunc(func(service, region string, opts ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{URL: endpoint}, nil
			}),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("r2: failed to load config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	return &R2Service{
		client:     client,
		bucket:     bucket,
		publicBase: strings.TrimRight(publicBase, "/"),
	}, nil
}

// UploadFile uploads a multipart file to R2 under the given folder prefix.
// Returns the full public URL of the uploaded object.
//
//	url, err := r2.UploadFile(fileHeader, "categories")
func (r *R2Service) UploadFile(fh *multipart.FileHeader, folder string) (string, error) {
	file, err := fh.Open()
	if err != nil {
		return "", fmt.Errorf("r2: cannot open file: %w", err)
	}
	defer file.Close()

	ext      := strings.ToLower(filepath.Ext(fh.Filename))
	key      := fmt.Sprintf("%s/%d%s", folder, time.Now().UnixNano(), ext)
	mimeType := fh.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	_, err = r.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(r.bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String(mimeType),
		// make publicly readable — remove if bucket is private
		ACL: "public-read",
	})
	if err != nil {
		return "", fmt.Errorf("r2: upload failed: %w", err)
	}

	return fmt.Sprintf("%s/%s", r.publicBase, key), nil
}

// DeleteFile removes an object from R2 by its full public URL.
func (r *R2Service) DeleteFile(publicURL string) error {
	key := strings.TrimPrefix(publicURL, r.publicBase+"/")
	_, err := r.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	return err
}