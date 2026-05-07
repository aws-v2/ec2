package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOAdapter struct {
	client *minio.Client
}

func NewMinIOAdapter(endpoint, accessKey, secretKey string, useSSL bool) (*MinIOAdapter, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}

	return &MinIOAdapter{client: client}, nil
}
func (m *MinIOAdapter) DownloadFile(ctx context.Context, bucket, key, destPath string) error {
	log.Printf("**[MinIO] Downloading: bucket=%s key=%s dest=%s", bucket, key, destPath)

	// add a hard timeout
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err := m.client.FGetObject(ctx, bucket, key, destPath, minio.GetObjectOptions{})
	if err != nil {
		log.Printf("**[MinIO] Download failed: bucket=%s key=%s dest=%s error=%v", bucket, key, destPath, err)
		return fmt.Errorf("failed to download %s/%s: %w", bucket, key, err)
	}

	log.Printf("**[MinIO] Download complete: bucket=%s key=%s dest=%s", bucket, key, destPath)
	return nil
}