package ocr

import "unicode"

// SplitTextBlocks 智能拆分文本块（与 Python 版本一致）
// 中文按单个字符拆分，英文按单词拆分
func SplitTextBlocks(blocks []TextBlock) []TextBlock {
	var result []TextBlock

	for _, block := range blocks {
		text := block.Text
		if len(text) <= 1 || block.Width <= 0 {
			result = append(result, block)
			continue
		}

		subBlocks := splitTextBlock(text, block.X, block.Y, block.Width, block.Height)
		result = append(result, subBlocks...)
	}

	return result
}

// splitTextBlock 拆分单个文本块
func splitTextBlock(text string, x, y, width, height int) []TextBlock {
	var result []TextBlock

	if len(text) == 0 || width <= 0 {
		return result
	}

	runes := []rune(text)

	// 计算加权字符宽度（全角字符算2个单位，半角算1个）
	totalUnits := 0
	for _, r := range runes {
		if isFullWidth(r) {
			totalUnits += 2
		} else {
			totalUnits += 1
		}
	}

	if totalUnits == 0 {
		return result
	}

	unitWidth := float64(width) / float64(totalUnits)

	// 计算每个字符的起始位置
	type charPos struct {
		startX float64
		width  float64
	}
	charPositions := make([]charPos, len(runes))
	currentX := 0.0
	for i, r := range runes {
		var charUnits int
		if isFullWidth(r) {
			charUnits = 2
		} else {
			charUnits = 1
		}
		charW := float64(charUnits) * unitWidth
		charPositions[i] = charPos{startX: currentX, width: charW}
		currentX += charW
	}

	// 分段处理：将文本分成中文段和英文单词段
	type segment struct {
		text      string
		startIdx  int
		isChinese bool
	}
	var segments []segment

	currentSegment := ""
	currentStart := 0
	var currentIsChinese *bool

	for i, r := range runes {
		charIsChinese := isChinese(r)
		isSeparator := r == ' ' || (!charIsChinese && !unicode.IsLetter(r) && !unicode.IsDigit(r))

		if isSeparator {
			// 分隔符结束当前段
			if currentSegment != "" && currentIsChinese != nil {
				segments = append(segments, segment{
					text:      currentSegment,
					startIdx:  currentStart,
					isChinese: *currentIsChinese,
				})
				currentSegment = ""
			}
			currentIsChinese = nil
		} else if currentIsChinese == nil {
			// 开始新段
			currentSegment = string(r)
			currentStart = i
			currentIsChinese = &charIsChinese
		} else if charIsChinese == *currentIsChinese {
			// 继续当前段
			currentSegment += string(r)
		} else {
			// 中英文切换，结束当前段
			if currentSegment != "" {
				segments = append(segments, segment{
					text:      currentSegment,
					startIdx:  currentStart,
					isChinese: *currentIsChinese,
				})
			}
			currentSegment = string(r)
			currentStart = i
			currentIsChinese = &charIsChinese
		}
	}

	// 添加最后一段
	if currentSegment != "" && currentIsChinese != nil {
		segments = append(segments, segment{
			text:      currentSegment,
			startIdx:  currentStart,
			isChinese: *currentIsChinese,
		})
	}

	// 处理每个段
	for _, seg := range segments {
		segRunes := []rune(seg.text)
		if seg.isChinese {
			// 中文：按单个字符拆分
			for i, r := range segRunes {
				idx := seg.startIdx + i
				if idx < len(charPositions) {
					charStartX := charPositions[idx].startX
					charW := charPositions[idx].width
					result = append(result, TextBlock{
						Text:   string(r),
						X:      x + int(charStartX),
						Y:      y,
						Width:  max(int(charW), 1),
						Height: height,
					})
				}
			}
		} else {
			// 英文：整个单词作为一个块
			if seg.startIdx < len(charPositions) {
				wordStartX := charPositions[seg.startIdx].startX
				wordEndIdx := seg.startIdx + len(segRunes) - 1
				if wordEndIdx >= len(charPositions) {
					wordEndIdx = len(charPositions) - 1
				}
				wordEndX := charPositions[wordEndIdx].startX + charPositions[wordEndIdx].width
				wordWidth := wordEndX - wordStartX
				result = append(result, TextBlock{
					Text:   seg.text,
					X:      x + int(wordStartX),
					Y:      y,
					Width:  max(int(wordWidth), 1),
					Height: height,
				})
			}
		}
	}

	return result
}

// isChinese 检查是否是中文字符
func isChinese(r rune) bool {
	return r >= '\u4e00' && r <= '\u9fff'
}

// isFullWidth 检查是否是全角字符
func isFullWidth(r rune) bool {
	return (r >= '\u4e00' && r <= '\u9fff') || // CJK 统一汉字
		(r >= '\u3000' && r <= '\u303f') || // CJK 标点
		(r >= '\uff00' && r <= '\uffef') || // 全角字符
		(r >= '\u3040' && r <= '\u309f') || // 平假名
		(r >= '\u30a0' && r <= '\u30ff') // 片假名
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

