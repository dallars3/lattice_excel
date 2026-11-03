// file/get_thumb.go：图片解码、128px 等比缩放与 JPEG 流的生成。
package file

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"

	filev1 "excel/api/file/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
)

// GetThumb streams a maximum-128px JPEG thumbnail for image attachments.
func (service *Service) GetThumb(
	request *filev1.GetThumbRequest,
	stream filev1.FileService_GetThumbServer,
) error {
	if service == nil || service.repository == nil {
		return rpcError(deal.CodeServiceUnavailable)
	}
	if request == nil || request.GetFileId() == "" {
		return rpcError(deal.CodeInvalidArgument)
	}
	if _, err := principalFromContext(stream.Context()); err != nil {
		return err
	}
	meta, err := service.repository.Get(stream.Context(), request.GetFileId())
	if errors.Is(err, base.ErrFileNotFound) {
		return rpcError(deal.CodeDeletedResource)
	}
	if err != nil {
		return err
	}
	if generatedFile(meta) {
		return rpcError(deal.CodeInvalidArgument)
	}
	source, err := os.Open(filepath.Join(service.attachDir, meta.StoragePath))
	if err != nil {
		return err
	}
	defer source.Close()

	var decoded image.Image
	switch normalizeMIME(meta.Mime) {
	case "image/jpeg":
		decoded, err = jpeg.Decode(source)
	case "image/png":
		decoded, err = png.Decode(source)
	case "image/gif":
		decoded, err = gif.Decode(source)
	default:
		return rpcError(deal.CodeInvalidArgument)
	}
	if err != nil {
		return rpcError(deal.CodeInvalidArgument)
	}
	thumbnail := resizeNearest(decoded, 128)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, thumbnail, &jpeg.Options{Quality: 85}); err != nil {
		return err
	}
	setFileHeaders(stream.Context(), meta.DisplayName+".jpg", "image/jpeg")
	return streamChunks(bytes.NewReader(encoded.Bytes()), func(chunk []byte) error {
		return stream.Send(&filev1.FileChunk{Chunk: chunk})
	})
}

func resizeNearest(source image.Image, maximum int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= maximum && height <= maximum {
		return source
	}
	targetWidth, targetHeight := width, height
	if width >= height {
		targetWidth = maximum
		targetHeight = maxInt(1, height*maximum/width)
	} else {
		targetHeight = maximum
		targetWidth = maxInt(1, width*maximum/height)
	}
	target := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	for y := 0; y < targetHeight; y++ {
		sourceY := bounds.Min.Y + y*height/targetHeight
		for x := 0; x < targetWidth; x++ {
			sourceX := bounds.Min.X + x*width/targetWidth
			red, green, blue, alpha := source.At(sourceX, sourceY).RGBA()
			target.Set(x, y, color.RGBA{
				R: uint8(red >> 8),
				G: uint8(green >> 8),
				B: uint8(blue >> 8),
				A: uint8(alpha >> 8),
			})
		}
	}
	return target
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
