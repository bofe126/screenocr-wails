package ocr

import (
	"image"
	"image/color"
)

// preprocessImage 图像预处理（增强对比度）
func preprocessImage(img image.Image) image.Image {
	bounds := img.Bounds()
	result := image.NewRGBA(bounds)

	// 增强对比度 1.5 倍
	contrastFactor := 1.5

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			originalColor := img.At(x, y)
			r, g, b, a := originalColor.RGBA()

			// 转换为 0-255 范围
			r8 := float64(r >> 8)
			g8 := float64(g >> 8)
			b8 := float64(b >> 8)

			// 增强对比度
			r8 = clamp((r8-128)*contrastFactor+128, 0, 255)
			g8 = clamp((g8-128)*contrastFactor+128, 0, 255)
			b8 = clamp((b8-128)*contrastFactor+128, 0, 255)

			result.Set(x, y, color.RGBA{
				R: uint8(r8),
				G: uint8(g8),
				B: uint8(b8),
				A: uint8(a >> 8),
			})
		}
	}

	return result
}

func clamp(val, min, max float64) float64 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

