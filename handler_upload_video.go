package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
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

	aspectFromFile, err := getVideoAspectRatio(tempFile.Name())
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

	provessedVideo, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to process video for fast start", err)
		return
	}
	defer os.Remove(provessedVideo)

	tempFile, err = os.Open(provessedVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to open processed video", err)
		return
	}
	defer tempFile.Close()

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

	//dataURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s/%s",
	//	cfg.s3Bucket, cfg.s3Region, aspectPrefix, filename)
	bucketKey := fmt.Sprintf("%s,%s/%s", cfg.s3Bucket, aspectPrefix, filename)
	videoMetadata.UpdatedAt = time.Now()
	videoMetadata.VideoURL = &bucketKey

	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video metadata", err)
		return
	}

	videoMetadataSigned, err := cfg.dbVideoToSignedVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to generate signed video URL", err)
		return
	}
	videoMetadata = videoMetadataSigned

	respondWithJSON(w, http.StatusOK, videoMetadata)
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s3Client)

	presignRequest, err := presignClient.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}

	return presignRequest.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}

	parts := strings.SplitN(*video.VideoURL, ",", 2)
	if len(parts) != 2 {
		return video, fmt.Errorf("invalid stored video url format")
	}
	bucket := parts[0]
	key := parts[1]
	fmt.Printf("%s, %s\n", key, bucket)

	presignedURL, err := generatePresignedURL(cfg.s3Client, bucket, key, 15*time.Minute)
	if err != nil {
		return video, err
	}

	videoSigned := video
	videoSigned.VideoURL = &presignedURL
	return videoSigned, nil
}
