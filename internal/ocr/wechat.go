//go:build windows

package ocr

import (
	"fmt"
	"image"
)

// WeChatOCR 微信 OCR 引擎
// 注意：WeChatOCR 需要调用本地微信的 OCR 组件
// 这里提供占位实现，实际使用需要 CGO 调用 wcocr.dll
type WeChatOCR struct {
	available bool
	errorMsg  string
}

// NewWeChatOCR 创建 WeChatOCR 实例
func NewWeChatOCR() *WeChatOCR {
	ocr := &WeChatOCR{}
	ocr.init()
	return ocr
}

// init 初始化
func (w *WeChatOCR) init() {
	// WeChatOCR 需要 wcocr.dll 和本地微信安装
	// 这里标记为不可用，推荐使用 Windows OCR
	w.available = false
	w.errorMsg = "WeChatOCR 需要 CGO 支持和本地微信安装，建议使用 Windows OCR"
	fmt.Println("⚠ WeChatOCR: " + w.errorMsg)
}

// IsAvailable 检查是否可用
func (w *WeChatOCR) IsAvailable() bool {
	return w.available
}

// Recognize 识别图片
func (w *WeChatOCR) Recognize(img image.Image, preprocess bool) ([]TextBlock, error) {
	if !w.available {
		return nil, fmt.Errorf("WeChatOCR 不可用: %s", w.errorMsg)
	}

	// TODO: 实现 WeChatOCR 调用
	// 需要 CGO 调用 wcocr.dll
	return nil, fmt.Errorf("WeChatOCR 未实现")
}

// GetError 获取错误信息
func (w *WeChatOCR) GetError() string {
	return w.errorMsg
}

// Close 关闭
func (w *WeChatOCR) Close() {
	// 无需清理
}

