package ocr

import "image"

// TextBlock 文字块
type TextBlock struct {
	Text   string `json:"text"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Engine OCR 引擎接口
type Engine interface {
	// IsAvailable 检查是否可用
	IsAvailable() bool

	// Recognize 识别图片中的文字
	Recognize(img image.Image, preprocess bool) ([]TextBlock, error)

	// GetError 获取错误信息
	GetError() string

	// Close 关闭引擎
	Close()
}

