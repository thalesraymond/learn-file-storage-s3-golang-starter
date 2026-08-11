package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
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

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video from database", err)
		return
	}

	if video.UserID != userID {
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

	bytes := make([]byte, 32)
	_, err = rand.Read(bytes)
	finalFileName := base64.RawURLEncoding.EncodeToString(bytes) + fileExtension

	_, err = cfg.s3Config.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &finalFileName,
		Body:        tempFile,
		ContentType: &contentType,
	})

	if err != nil {
		fmt.Println(err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload file to S3", err)
		return
	}

	videoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, finalFileName)
	video.VideoURL = &videoURL

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video in database", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
