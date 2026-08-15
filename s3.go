package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

// uploadVideoToS3 stores the video in S3 under a random key with the given
// prefix and returns the "bucket,key" reference used by the database.
func (cfg *apiConfig) uploadVideoToS3(ctx context.Context, video io.Reader, contentType, keyPrefix, fileExtension string) (string, error) {
	key, err := randomObjectKey(keyPrefix, fileExtension)
	if err != nil {
		return "", err
	}

	_, err = cfg.s3Config.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &key,
		Body:        video,
		ContentType: &contentType,
	})
	if err != nil {
		return "", fmt.Errorf("couldn't upload file to S3: %w", err)
	}

	return fmt.Sprintf("%s,%s", cfg.s3Bucket, key), nil
}

// randomObjectKey returns a random object key under keyPrefix with the given
// file extension.
func randomObjectKey(keyPrefix, fileExtension string) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("couldn't generate random bytes: %w", err)
	}

	return keyPrefix + "/" + base64.RawURLEncoding.EncodeToString(bytes) + fileExtension, nil
}

// dbVideoToSignedVideo replaces the stored "bucket,key" video reference with
// a short-lived presigned URL so the client can fetch the video directly.
func (cfg *apiConfig) dbVideoToSignedVideo(video *database.Video) (database.Video, error) {
	bucket, key, found := strings.Cut(*video.VideoURL, ",")
	if !found {
		return database.Video{}, fmt.Errorf("invalid video URL: %s", *video.VideoURL)
	}

	psClient := s3.NewPresignClient(cfg.s3Config)
	req, err := psClient.PresignGetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}, s3.WithPresignExpires(15*time.Minute))
	if err != nil {
		fmt.Println("Error generating presigned URL:", err)
		return database.Video{}, err
	}

	video.VideoURL = &req.URL
	return *video, nil
}
