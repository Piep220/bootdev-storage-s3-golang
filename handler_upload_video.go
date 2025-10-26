package main

import (
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"fmt"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const uploadLimit = 1 << 30
	videoIDString := r.PathValue("videoID")
	videoId, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit)

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

	videoMetadata, err := cfg.db.GetVideo(videoId)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video metadata", err)
		return
	}
	if videoMetadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You do not have permission to upload video for this ID", nil)
		return
	}

	file, fileHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(fileHeader.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse media type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Unsupported file type. Only MP4 is allowed.", nil)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload-temp.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temp file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to save temp file", err)
		return
	}

	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to seek temp file", err)
		return
	}

	aspectFromFile , err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video aspect ratio", err)
		return
	}

	aspectPrefix := "other"
	switch aspectFromFile {
	case "16:9":
		aspectPrefix = "landscape"
	case "9:16":
		aspectPrefix = "portrait"
	}

	randKey := make([]byte, 32)
	rand.Read(randKey)
	randID := base64.RawURLEncoding.EncodeToString(randKey)
	fileExtension := "mp4"
	filename := fmt.Sprintf("%s.%s", randID, fileExtension)

	_, err = cfg.s3Client.PutObject(
		context.Background(), &s3.PutObjectInput{
			Bucket:      aws.String(cfg.s3Bucket),
			Key:         aws.String(fmt.Sprintf("%s/%s", aspectPrefix, filename)),
			Body:        tempFile,
			ContentType: aws.String("video/mp4"),
		},
	)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to upload file to S3", err)
		return
	}

	dataURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s/%s", 
		cfg.s3Bucket, cfg.s3Region, aspectPrefix, filename)
	videoMetadata.UpdatedAt =  time.Now()
	videoMetadata.VideoURL = &dataURL

	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video metadata", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoMetadata)
}
