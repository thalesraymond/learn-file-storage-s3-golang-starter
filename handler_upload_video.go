package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/video"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	http.MaxBytesReader(w, r.Body, 1<<30)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video from database", err)
		return
	}

	if dbVideo.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You don't have permission to upload a thumbnail for this video", nil)
		return
	}

	r.ParseMultipartForm(10 << 30) // 1GB
	file, _, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get video file", err)
		return
	}

	defer file.Close()

	fileBytes := make([]byte, 0)
	buffer := make([]byte, 1024)
	for {
		n, err := file.Read(buffer)
		if err != nil {
			break
		}
		fileBytes = append(fileBytes, buffer[:n]...)
	}

	fileExtension := ""
	contentType := http.DetectContentType(fileBytes)
	switch contentType {
	case "video/mp4":
		fileExtension = ".mp4"
	default:
		respondWithError(w, http.StatusBadRequest, "Invalid file type", nil)
		return
	}

	tempFileName := "upload-" + videoID.String() + "-video" + fileExtension
	tempFile, err := os.CreateTemp("", tempFileName)

	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := tempFile.Write(fileBytes); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't write file to disk", err)
		return
	}

	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't seek file to beginning", err)
		return
	}

	processedVideoPath, err := video.ProcessForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't process video for fast start", err)
		return
	}

	aspectRatio, err := video.GetAspectRatio(processedVideoPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get aspect ratio", err)
		return
	}

	keyPrefix := "other"
	switch aspectRatio {
	case "16:9":
		keyPrefix = "landscape"
	case "9:16":
		keyPrefix = "portrait"
	}

	bytes := make([]byte, 32)
	_, err = rand.Read(bytes)
	finalFileName := keyPrefix + "/" + base64.RawURLEncoding.EncodeToString(bytes) + fileExtension

	processedVideoFile, err := os.Open(processedVideoPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't open processed video file", err)
		return
	}

	defer processedVideoFile.Close()

	_, err = cfg.s3Config.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &finalFileName,
		Body:        processedVideoFile,
		ContentType: &contentType,
	})

	if err != nil {
		fmt.Println(err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload file to S3", err)
		return
	}

	videoURL := fmt.Sprintf("%s,%s", cfg.s3Bucket, finalFileName)
	dbVideo.VideoURL = &videoURL

	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video in database", err)
		return
	}

	signedURLVideo, err := cfg.dbVideoToSignedVideo(&dbVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't generate signed URL for video", err)
		return
	}
	fmt.Print(signedURLVideo.VideoURL)
	respondWithJSON(w, http.StatusOK, signedURLVideo)
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	psClient := s3.NewPresignClient(s3Client)
	req, err := psClient.PresignGetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}

	return req.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video *database.Video) (database.Video, error) {
	data := strings.Split(*video.VideoURL, ",")

	signedURL, err := generatePresignedURL(cfg.s3Config, data[0], data[1], 15*time.Minute)
	if err != nil {
		fmt.Println("Error generating presigned URL:", err)
		return database.Video{}, err
	}

	video.VideoURL = &signedURL
	return *video, nil
}
