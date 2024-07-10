package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/image/webp"
)

const (
	port         = 8080
	cbzDirectory = "./" // Directory where .cbz files are stored
)

func main() {
	http.HandleFunc("/webtoon", handleWebtoon)

	log.Printf("Server starting on port %d...\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func handleWebtoon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filename := r.URL.Query().Get("file")
	if filename == "" {
		http.Error(w, "File parameter is required", http.StatusBadRequest)
		return
	}

	if filepath.Ext(filename) != ".cbz" {
		http.Error(w, "Invalid file extension. Only .cbz files are allowed", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(cbzDirectory, filepath.Clean(filename))

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	img, err := CreateWebtoonStrip(filePath)
	if err != nil {
		log.Printf("Error creating webtoon strip: %v", err)
		http.Error(w, fmt.Sprintf("Error processing file: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s.png\"", filepath.Base(filename)))

	err = streamPNG(w, img)
	if err != nil {
		log.Printf("Error streaming PNG: %v", err)
		http.Error(w, "Error sending image", http.StatusInternalServerError)
		return
	}
}

func CreateWebtoonStrip(cbzFilePath string) (image.Image, error) {
	reader, err := zip.OpenReader(cbzFilePath)
	if err != nil {
		return nil, fmt.Errorf("error opening CBZ file: %v", err)
	}
	defer reader.Close()

	sort.Slice(reader.File, func(i, j int) bool {
		return reader.File[i].Name < reader.File[j].Name
	})

	var images []image.Image
	var totalHeight int
	var commonWidth int

	for _, file := range reader.File {
		if isImageFile(file.Name) {
			rc, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("error opening file %s: %v", file.Name, err)
			}

			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("error reading file %s: %v", file.Name, err)
			}

			img, format, err := decodeImage(bytes.NewReader(data))
			if err != nil {
				log.Printf("Error decoding file %s: %v", file.Name, err)
				continue // Skip this file and try the next one
			}

			log.Printf("Successfully decoded %s as %s", file.Name, format)

			width := img.Bounds().Dx()
			if commonWidth == 0 {
				commonWidth = width
			} else if width != commonWidth {
				log.Printf("Skipping %s: width %d doesn't match common width %d", file.Name, width, commonWidth)
				continue
			}

			images = append(images, img)
			totalHeight += img.Bounds().Dy()
		}
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("no valid images found with matching width in the CBZ file")
	}

	finalImage := image.NewRGBA(image.Rect(0, 0, commonWidth, totalHeight))
	currentY := 0

	for _, img := range images {
		draw.Draw(finalImage, image.Rect(0, currentY, commonWidth, currentY+img.Bounds().Dy()), img, image.Point{}, draw.Src)
		currentY += img.Bounds().Dy()
	}

	return finalImage, nil
}

func isImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp"
}

func decodeImage(r io.Reader) (image.Image, string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, "", fmt.Errorf("error reading image data: %v", err)
	}

	// Try decoding as JPEG
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err == nil {
		return img, "jpeg", nil
	}

	// Try decoding as PNG
	img, err = png.Decode(bytes.NewReader(data))
	if err == nil {
		return img, "png", nil
	}

	// Try decoding as WebP
	img, err = webp.Decode(bytes.NewReader(data))
	if err == nil {
		return img, "webp", nil
	}

	return nil, "", fmt.Errorf("unsupported image format")
}

func streamPNG(w io.Writer, img image.Image) error {
	encoder := png.Encoder{
		CompressionLevel: png.DefaultCompression,
	}
	return encoder.Encode(w, img)
}
